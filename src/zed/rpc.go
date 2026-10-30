package zed

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"zedex/utils"
	"zedex/zed/pb"

	"github.com/0x6flab/namegenerator"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	ZSTD_COMPRESSION_LEVEL = 4
	WEBSOCKET_READ_LIMIT   = 1024 * 1024
	// KEEPALIVE_INTERVAL is how often we ping the client. Zed drops collab
	// connections that stay silent for 10 seconds.
	KEEPALIVE_INTERVAL = 5 * time.Second
)

// zstdMagic is the frame marker of a zstd stream (RFC 8878).
var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

type RpcHandler struct {
	// connections holds every open client connection, keyed by connection id.
	connections utils.ConcurrentMap[uint32, *connection]
	// connectionsByPeer maps a peer id to its connection.
	connectionsByPeer utils.ConcurrentMap[peerKey, uint32]
	// userConnections maps a user id to the connections it has open.
	userConnections utils.ConcurrentMap[uint64, []uint32]
	// nextConnectionId hands out connection ids.
	nextConnectionId utils.ConcurrentCounter[uint32]

	users            utils.ConcurrentMap[uint64, *pb.User]
	channels         utils.ConcurrentMap[uint64, *pb.Channel]
	channelMembers   utils.ConcurrentMap[uint64, []*pb.ChannelMember]
	channelMessages  utils.ConcurrentMap[uint64, []*pb.ChannelMessage]
	channelBuffers   utils.ConcurrentMap[uint64, *channelBufferState]
	projectBuffers   utils.ConcurrentMap[uint64, *projectBufferState]
	projectBufferIds *projectBufferIndex
	id               utils.ConcurrentCounter[uint32]
	nameGenerator    namegenerator.NameGenerator
	notifications    *notificationsStore
	contacts         *contactsStore
	projects         *projectsStore
	rooms            *roomsStore
	pendingForwards  utils.ConcurrentMap[uint32, forwardedRequest]
	// port is the port the server listens on, used to build avatar URLs.
	port int
}

// channelBufferState holds the shared state of a channel's notes buffer.
//
// Zed's channel notes use a CRDT: every client owns a replica id and sends
// operations that carry a version vector. The server keeps the operation
// history so that clients joining later can rebuild the same state, and
// relays operations to the other collaborators of the channel.
type channelBufferState struct {
	mu            sync.Mutex
	baseText      string
	epoch         uint64
	operations    []*pb.Operation
	version       []*pb.VectorClockEntry
	collaborators map[uint64]*pb.Collaborator
	nextReplicaId uint32
}

// FIRST_COLLAB_ID mirrors clock::ReplicaId::FIRST_COLLAB_ID: replica ids below
// this value are reserved for local/agent/remote-server use.
const FIRST_COLLAB_ID = 8

func newChannelBufferState() *channelBufferState {
	return &channelBufferState{
		baseText:      "",
		epoch:         1,
		operations:    []*pb.Operation{},
		version:       []*pb.VectorClockEntry{},
		collaborators: map[uint64]*pb.Collaborator{},
		nextReplicaId: FIRST_COLLAB_ID,
	}
}

// addCollaborator registers a user in the buffer and assigns it a unique
// replica id, returning the collaborator entry.
func (s *channelBufferState) addCollaborator(userId uint64, peerId *pb.PeerId) *pb.Collaborator {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.collaborators[userId]; ok {
		return existing
	}
	replicaId := s.nextReplicaId
	s.nextReplicaId++
	collaborator := &pb.Collaborator{
		PeerId:    peerId,
		ReplicaId: replicaId,
		UserId:    userId,
	}
	s.collaborators[userId] = collaborator
	return collaborator
}

func (s *channelBufferState) removeCollaborator(userId uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collaborators, userId)
}

func (s *channelBufferState) collaboratorList() []*pb.Collaborator {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*pb.Collaborator, 0, len(s.collaborators))
	for _, c := range s.collaborators {
		out = append(out, c)
	}
	return out
}

func (s *channelBufferState) collaboratorUserIds() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint64, 0, len(s.collaborators))
	for id := range s.collaborators {
		out = append(out, id)
	}
	return out
}

// recordOperations appends operations to the buffer history and advances the
// version vector accordingly.
func (s *channelBufferState) recordOperations(ops []*pb.Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		if op == nil {
			continue
		}
		s.operations = append(s.operations, op)
		switch v := op.Variant.(type) {
		case *pb.Operation_Edit_:
			edit := v.Edit
			if edit == nil {
				continue
			}
			s.observeVersion(edit.ReplicaId, edit.LamportTimestamp)
			for _, entry := range edit.Version {
				s.observeVersion(entry.ReplicaId, entry.Timestamp)
			}
		case *pb.Operation_Undo_:
			undo := v.Undo
			if undo == nil {
				continue
			}
			s.observeVersion(undo.ReplicaId, undo.LamportTimestamp)
			for _, entry := range undo.Version {
				s.observeVersion(entry.ReplicaId, entry.Timestamp)
			}
		}
	}
}

func (s *channelBufferState) observeVersion(replicaId uint32, timestamp uint32) {
	for _, entry := range s.version {
		if entry.ReplicaId == replicaId {
			if entry.Timestamp < timestamp {
				entry.Timestamp = timestamp
			}
			return
		}
	}
	s.version = append(s.version, &pb.VectorClockEntry{ReplicaId: replicaId, Timestamp: timestamp})
}

func (s *channelBufferState) snapshot() (string, uint64, []*pb.Operation, []*pb.VectorClockEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := make([]*pb.Operation, len(s.operations))
	copy(ops, s.operations)
	version := make([]*pb.VectorClockEntry, len(s.version))
	copy(version, s.version)
	return s.baseText, s.epoch, ops, version
}

func NewRpcHandler() RpcHandler {
	return RpcHandler{
		connections:       utils.NewConcurrentMap[uint32, *connection](),
		connectionsByPeer: utils.NewConcurrentMap[peerKey, uint32](),
		userConnections:   utils.NewConcurrentMap[uint64, []uint32](),
		nextConnectionId:  utils.NewConcurrentCounter[uint32](),
		users:             utils.NewConcurrentMap[uint64, *pb.User](),
		channels:          utils.NewConcurrentMap[uint64, *pb.Channel](),
		channelMembers:    utils.NewConcurrentMap[uint64, []*pb.ChannelMember](),
		channelMessages:   utils.NewConcurrentMap[uint64, []*pb.ChannelMessage](),
		channelBuffers:    utils.NewConcurrentMap[uint64, *channelBufferState](),
		projectBuffers:    utils.NewConcurrentMap[uint64, *projectBufferState](),
		projectBufferIds:  newProjectBufferIndex(),
		id:                utils.NewConcurrentCounter[uint32](),
		nameGenerator:     namegenerator.NewGenerator(),
		notifications:     newNotificationsStore(),
		contacts:          newContactsStore(),
		projects:          newProjectsStore(),
		rooms:             newRoomsStore(),
		pendingForwards:   utils.NewConcurrentMap[uint32, forwardedRequest](),
	}
}

// ProtoDispatcher sends envelopes to a single client connection.
type ProtoDispatcher struct {
	rpc *RpcHandler
	// connId is the connection the envelopes are sent to.
	connId uint32
	// userId is the user the connection belongs to.
	userId int
	peerId *pb.PeerId
}

// NewProtoDispatcher returns a dispatcher for a user. When the user has
// several connections open, the most recent one is used.
func NewProtoDispatcher(rpc *RpcHandler, userId int) *ProtoDispatcher {
	connId := rpc.latestConnectionId(uint64(userId))
	pd := &ProtoDispatcher{
		rpc:    rpc,
		connId: connId,
		userId: userId,
	}
	if conn := rpc.connections.Get(connId); conn != nil {
		pd.peerId = conn.peerId
	}
	return pd
}

// NewProtoDispatcherForConnection returns a dispatcher for a specific
// connection.
func NewProtoDispatcherForConnection(rpc *RpcHandler, connId uint32) *ProtoDispatcher {
	conn := rpc.connections.Get(connId)
	if conn == nil {
		return nil
	}
	return &ProtoDispatcher{
		rpc:    rpc,
		connId: connId,
		userId: int(conn.userId),
		peerId: conn.peerId,
	}
}

func (pd *ProtoDispatcher) CompressMsg(b []byte) ([]byte, error) {
	return CompressMsg(b)
}

// CompressMsg zstd-compresses an RPC frame the same way Zed does.
func CompressMsg(b []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel((ZSTD_COMPRESSION_LEVEL)))
	if err != nil {
		return []byte{}, err
	}
	return encoder.EncodeAll(b, make([]byte, 0, len(b))), nil
}

func (pd *ProtoDispatcher) SendMessage(b []byte) error {
	bb, err := pd.CompressMsg(b)
	if err != nil {
		return err
	}
	conn := pd.rpc.connections.Get(pd.connId)
	if conn == nil {
		return fmt.Errorf("connection %v is closed", pd.connId)
	}
	if err := conn.writeCompressed(bb); err != nil {
		return err
	}
	return nil
}

func (pd *ProtoDispatcher) SendProtobuf(protobuf protoreflect.ProtoMessage) error {
	log.Infof("[user: %v] sending proto %#v", pd.userId, protobuf)
	b, err := proto.Marshal(protobuf)
	if err != nil {
		return err
	}
	return pd.SendMessage(b)
}

func (pd *ProtoDispatcher) SendHello() error {
	peerId := pd.peerId
	if peerId == nil {
		peerId = &pb.PeerId{Id: uint32(pd.userId)}
	}
	envelope := pb.Envelope{
		Payload: &pb.Envelope_Hello{
			Hello: &pb.Hello{
				PeerId: peerId,
			},
		},
	}

	return pd.SendProtobuf(&envelope)
}

func (rpc *RpcHandler) NextId() uint32 {
	return rpc.id.Increment().Value()
}

func (pd *ProtoDispatcher) NextId() uint32 {
	return pd.rpc.NextId()
}

// channelsUpdate builds the channel list update that Zed expects, including
// the participants of every channel. Without the participant list the
// collaboration panel does not show who is in a channel.
func (rpc *RpcHandler) channelsUpdate() *pb.UpdateChannels {
	update := &pb.UpdateChannels{
		Channels: rpc.channels.Values(),
	}
	for _, channel := range rpc.channels.Values() {
		members := rpc.channelMembers.Get(channel.Id)
		participantIds := []uint64{}
		for _, member := range members {
			if member.Kind == pb.ChannelMember_Member {
				participantIds = append(participantIds, member.UserId)
			}
		}
		if len(participantIds) == 0 {
			continue
		}
		update.ChannelParticipants = append(update.ChannelParticipants, &pb.ChannelParticipants{
			ChannelId:          channel.Id,
			ParticipantUserIds: participantIds,
		})
	}
	return update
}

// avatarURL returns the URL of the avatar that zedex serves for a user.
func (rpc *RpcHandler) avatarURL(userId uint64) string {
	base := utils.EnvWithFallback("BASE_URL", "")
	if base == "" {
		base = "http://127.0.0.1:" + strconv.Itoa(rpc.port)
	}
	return fmt.Sprintf("%s/avatar/%d", base, userId)
}

func (rpc *RpcHandler) broadcastEnvelope(env *pb.Envelope, except int) {
	for _, connId := range rpc.connections.Keys() {
		conn := rpc.connections.Get(connId)
		if conn == nil || int(conn.userId) == except {
			continue
		}
		pd := NewProtoDispatcherForConnection(rpc, connId)
		if pd == nil {
			continue
		}
		if err := pd.SendProtobuf(env); err != nil {
			log.Errorf("broadcast failed for connection %v: %v", connId, err)
		}
	}
}

// broadcastToChannelCollaborators sends an envelope to every client that has
// joined the notes buffer of the given channel, except the sender.
func (rpc *RpcHandler) broadcastToChannelCollaborators(channelId uint64, env *pb.Envelope, except int) {
	state := rpc.channelBuffers.Get(channelId)
	if state == nil {
		return
	}
	for _, uid := range state.collaboratorUserIds() {
		if int(uid) == except {
			continue
		}
		for _, connId := range rpc.userConnections.Get(uid) {
			pd := NewProtoDispatcherForConnection(rpc, connId)
			if pd == nil {
				continue
			}
			if err := pd.SendProtobuf(env); err != nil {
				log.Errorf("broadcast failed for connection %v: %v", connId, err)
			}
		}
	}
}

func (rpc *RpcHandler) usersForChannel(channelId uint64) []*pb.User {
	members := rpc.channelMembers.Get(channelId)
	users := []*pb.User{}
	for _, m := range members {
		u := rpc.users.Get(m.UserId)
		if u == nil {
			name, _, _ := identityFor(m.UserId)
			u = &pb.User{
				Id:          m.UserId,
				Username:    name,
				GithubLogin: name,
			}
			rpc.users.Set(m.UserId, u)
		}
		users = append(users, u)
	}
	return users
}

func containsUint64(list []uint64, v uint64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func collabProtos(ids []uint64) []*pb.Collaborator {
	out := []*pb.Collaborator{}
	for _, id := range ids {
		out = append(out, &pb.Collaborator{
			PeerId:    &pb.PeerId{Id: uint32(id)},
			ReplicaId: uint32(id),
			UserId:    id,
		})
	}
	return out
}

func (rpc *RpcHandler) membershipsForUser(userId uint64) []*pb.ChannelMembership {
	memberships := []*pb.ChannelMembership{}
	for _, ch := range rpc.channels.Values() {
		for _, mem := range rpc.channelMembers.Get(ch.Id) {
			if mem.UserId != userId {
				continue
			}
			if mem.Kind == pb.ChannelMember_Invitee {
				continue
			}
			memberships = append(memberships, &pb.ChannelMembership{
				ChannelId: ch.Id,
				Role:      mem.Role,
			})
			break
		}
	}
	return memberships
}

func (rpc *RpcHandler) sendUserChannels(userId uint64) {
	pd := NewProtoDispatcher(rpc, int(userId))
	env := pb.Envelope{
		Id: pd.NextId(),
		Payload: &pb.Envelope_UpdateUserChannels{
			UpdateUserChannels: &pb.UpdateUserChannels{
				ChannelMemberships: rpc.membershipsForUser(userId),
			},
		},
	}
	if err := pd.SendProtobuf(&env); err != nil {
		log.Errorf("failed to send user channels to %v: %v", userId, err)
	}
}

// CanonicalUserID normalizes a user id so that the cloud API (which truncates
// ids to u32) and the collab RPC (which uses u64) refer to the same user.
func CanonicalUserID(id uint64) uint64 {
	return id & 0x7fffffff
}

// contactFor builds the contact entry Zed expects for a user.
func (rpc *RpcHandler) contactFor(userId uint64) *pb.Contact {
	return &pb.Contact{
		UserId: userId,
		Online: len(rpc.userConnections.Get(userId)) > 0,
		Busy:   false,
	}
}

// sendContactsUpdate sends a contact list update to every connection of a
// user.
func (rpc *RpcHandler) sendContactsUpdate(userId uint64, update *pb.UpdateContacts) {
	for _, connId := range rpc.userConnections.Get(userId) {
		pd := NewProtoDispatcherForConnection(rpc, connId)
		if pd == nil {
			continue
		}
		env := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateContacts{
				UpdateContacts: update,
			},
		}
		if err := pd.SendProtobuf(&env); err != nil {
			log.Errorf("failed to send contacts update: %v", err)
		}
	}
}

func (rpc *RpcHandler) handleMessages(pd *ProtoDispatcher) {
	conn := rpc.connections.Get(pd.connId)
	if conn == nil {
		return
	}
	for {
		// Read via NextReader + io.Copy instead of ReadMessage: gorilla's
		// ReadMessage uses io.ReadAll, which panics when a frame is larger
		// than the connection's internal read buffer.
		messageType, reader, err := conn.socket.NextReader()
		if err != nil {
			log.Errorf("failed to read message: %v", err)
			return
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			log.Errorf("failed to read message body: %v", err)
			return
		}
		decoded, err := DecompressMsg(buf.Bytes())
		if err != nil {
			log.Errorf("failed to decompress message: %v", err)
			continue
		}
		rpc.handleMessage(pd, decoded)
	}
}

// DecompressMsg decodes a zstd-compressed RPC frame. Zed compresses every
// envelope it sends over the websocket, so incoming frames must be
// decompressed before they can be unmarshalled. Frames that are not
// compressed are passed through unchanged.
func DecompressMsg(b []byte) ([]byte, error) {
	if len(b) < 4 || !bytes.Equal(b[:4], zstdMagic) {
		return b, nil
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return decoder.DecodeAll(b, nil)
}

func (rpc *RpcHandler) handleMessage(pd *ProtoDispatcher, message []byte) error {
	var envelope pb.Envelope
	err := proto.Unmarshal(message, &envelope)
	if err != nil {
		log.Errorf("failed to unmarshal message: %v", err)
		return err
	}

	log.Debugf("[user: %v] incoming %#v", pd.userId, envelope.Payload)

	// Responses to requests that we forwarded to a project host are relayed
	// back to the original requester.
	if rpc.relayForwardedResponse(pd, &envelope) {
		return nil
	}

	switch msg := envelope.Payload.(type) {
	case *pb.Envelope_Hello:
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetUsers:
		gu := envelope.Payload.(*pb.Envelope_GetUsers)
		ru := []*pb.User{}
		for _, uid := range gu.GetUsers.UserIds {
			user := rpc.users.Get(uid)
			if user == nil {
				// The user is unknown to the server; synthesize an entry so
				// that the client can still show something.
				name, _, _ := identityFor(uid)
				user = &pb.User{
					Id:          uid,
					Username:    name,
					GithubLogin: name,
					AvatarUrl:   rpc.avatarURL(uid),
				}
				rpc.users.Set(uid, user)
			}
			ru = append(ru, user)
		}
		// Zed binary searches the user list, so it has to be sorted by id.
		sort.Slice(ru, func(i, j int) bool { return ru[i].Id < ru[j].Id })
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: ru,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMessagesById:
		ids := msg.GetChannelMessagesById.MessageIds
		msgs := []*pb.ChannelMessage{}
		for _, id := range ids {
			msgs = append(msgs, rpc.channelMessages.Get(id)...)
		}
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMessagesResponse{
				GetChannelMessagesResponse: &pb.GetChannelMessagesResponse{
					Messages: msgs,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SendChannelMessage:
		scm := msg.SendChannelMessage
		channelMsg := &pb.ChannelMessage{
			Id:               uint64(time.Now().UnixNano()),
			Body:             scm.Body,
			Timestamp:        uint64(time.Now().Unix()),
			Nonce:            scm.Nonce,
			Mentions:         scm.Mentions,
			ReplyToMessageId: scm.ReplyToMessageId,
		}

		rpc.channelMessages.Transaction(func(m map[uint64][]*pb.ChannelMessage) map[uint64][]*pb.ChannelMessage {
			m[scm.ChannelId] = append(m[scm.ChannelId], channelMsg)
			return m
		})

		broadcast := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_ChannelMessageSent{
				ChannelMessageSent: &pb.ChannelMessageSent{
					ChannelId: scm.ChannelId,
					Message:   channelMsg,
				},
			},
		}
		rpc.broadcastEnvelope(&broadcast, pd.userId)

		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_SendChannelMessageResponse{
				SendChannelMessageResponse: &pb.SendChannelMessageResponse{
					Message: channelMsg,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_AckChannelMessage:
		log.Debug("Envelope_AckChannelMessage")

	case *pb.Envelope_LeaveChannelChat:
		log.Debug("Envelope_LeaveChannelChat")

	case *pb.Envelope_JoinChannelBuffer:
		jcb := msg.JoinChannelBuffer
		state := rpc.channelBuffers.Get(jcb.ChannelId)
		if state == nil {
			state = newChannelBufferState()
			rpc.channelBuffers.Set(jcb.ChannelId, state)
		}
		userId := uint64(pd.userId)
		collaborator := state.addCollaborator(userId, pd.peerId)
		base, epoch, ops, _ := state.snapshot()
		collabs := state.collaboratorList()

		// Let the other collaborators know about the new participant.
		updateCollabs := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannelBufferCollaborators{
				UpdateChannelBufferCollaborators: &pb.UpdateChannelBufferCollaborators{
					ChannelId:     jcb.ChannelId,
					Collaborators: collabs,
				},
			},
		}
		rpc.broadcastToChannelCollaborators(jcb.ChannelId, &updateCollabs, pd.userId)

		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelBufferResponse{
				JoinChannelBufferResponse: &pb.JoinChannelBufferResponse{
					BufferId:      jcb.ChannelId,
					ReplicaId:     collaborator.ReplicaId,
					BaseText:      base,
					Operations:    ops,
					Epoch:         epoch,
					Collaborators: collabs,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetNotifications:
		gn := msg.GetNotifications
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetNotificationsResponse{
				GetNotificationsResponse: &pb.GetNotificationsResponse{
					Notifications: rpc.notifications.list(gn.GetBeforeId()),
					Done:          true,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_AddNotification:
		an := msg.AddNotification
		notification := rpc.notifications.add(an.GetNotification())
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateNotification{
				UpdateNotification: &pb.UpdateNotification{Notification: notification},
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_UpdateNotification:
		un := msg.UpdateNotification
		if notification := rpc.notifications.update(un.GetNotification()); notification != nil {
			update := pb.Envelope{
				Id: pd.NextId(),
				Payload: &pb.Envelope_UpdateNotification{
					UpdateNotification: &pb.UpdateNotification{Notification: notification},
				},
			}
			rpc.broadcastEnvelope(&update, pd.userId)
		}
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_DeleteNotification:
		dn := msg.DeleteNotification
		rpc.notifications.remove(dn.GetNotificationId())
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_MarkNotificationRead:
		mn := msg.MarkNotificationRead
		if notification := rpc.notifications.update(&pb.Notification{Id: mn.GetNotificationId(), IsRead: true}); notification != nil {
			update := pb.Envelope{
				Id: pd.NextId(),
				Payload: &pb.Envelope_UpdateNotification{
					UpdateNotification: &pb.UpdateNotification{Notification: notification},
				},
			}
			rpc.broadcastEnvelope(&update, pd.userId)
		}
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RequestContact:
		rc := msg.RequestContact
		responderId := rc.GetResponderId()
		requesterId := uint64(pd.userId)
		if responderId == requesterId {
			resp := pb.Envelope{
				RespondingTo: &envelope.Id,
				Payload: &pb.Envelope_Error{
					Error: &pb.Error{Message: "cannot add yourself as a contact"},
				},
			}
			return pd.SendProtobuf(&resp)
		}
		rpc.contacts.requestContact(requesterId, responderId)

		// The requester sees the request as outgoing, the responder as incoming.
		rpc.sendContactsUpdate(requesterId, &pb.UpdateContacts{
			OutgoingRequests: []uint64{responderId},
		})
		rpc.sendContactsUpdate(responderId, &pb.UpdateContacts{
			IncomingRequests: []*pb.IncomingContactRequest{{RequesterId: requesterId}},
		})

		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RespondToContactRequest:
		rr := msg.RespondToContactRequest
		requesterId := rr.GetRequesterId()
		responderId := uint64(pd.userId)
		accept := rr.GetResponse() == pb.ContactRequestResponse_Accept
		wasContact := rpc.contacts.respondToContactRequest(responderId, requesterId, accept)

		responderUpdate := &pb.UpdateContacts{
			RemoveIncomingRequests: []uint64{requesterId},
		}
		requesterUpdate := &pb.UpdateContacts{
			RemoveOutgoingRequests: []uint64{responderId},
		}
		if wasContact {
			responderUpdate.Contacts = []*pb.Contact{rpc.contactFor(requesterId)}
			requesterUpdate.Contacts = []*pb.Contact{rpc.contactFor(responderId)}
		}
		rpc.sendContactsUpdate(responderId, responderUpdate)
		rpc.sendContactsUpdate(requesterId, requesterUpdate)

		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RemoveContact:
		rmc := msg.RemoveContact
		otherId := rmc.GetUserId()
		userId := uint64(pd.userId)
		wasContact := rpc.contacts.removeContact(userId, otherId)

		userUpdate := &pb.UpdateContacts{RemoveOutgoingRequests: []uint64{otherId}}
		otherUpdate := &pb.UpdateContacts{RemoveIncomingRequests: []uint64{userId}}
		if wasContact {
			userUpdate = &pb.UpdateContacts{RemoveContacts: []uint64{otherId}}
			otherUpdate = &pb.UpdateContacts{RemoveContacts: []uint64{userId}}
		}
		rpc.sendContactsUpdate(userId, userUpdate)
		rpc.sendContactsUpdate(otherId, otherUpdate)

		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_ShowContacts:
		// The client asks the server for the current contact state. Zed binary
		// searches the contacts and requests by username, so both lists have
		// to be sorted by username.
		userId := uint64(pd.userId)
		update := &pb.UpdateContacts{}
		contacts := []*pb.Contact{}
		for _, id := range rpc.contacts.listContacts(userId) {
			contacts = append(contacts, rpc.contactFor(id))
		}
		sort.Slice(contacts, func(i, j int) bool {
			return contacts[i].GetUserId() < contacts[j].GetUserId()
		})
		update.Contacts = contacts

		incoming := []*pb.IncomingContactRequest{}
		for _, id := range rpc.contacts.listIncoming(userId) {
			incoming = append(incoming, &pb.IncomingContactRequest{RequesterId: id})
		}
		sort.Slice(incoming, func(i, j int) bool {
			return incoming[i].GetRequesterId() < incoming[j].GetRequesterId()
		})
		update.IncomingRequests = incoming

		outgoing := []uint64{}
		for _, id := range rpc.contacts.listOutgoing(userId) {
			outgoing = append(outgoing, id)
		}
		sort.Slice(outgoing, func(i, j int) bool { return outgoing[i] < outgoing[j] })
		update.OutgoingRequests = outgoing
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UpdateContacts{
				UpdateContacts: update,
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_Ping:
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_CreateRoom:
		return rpc.handleCreateRoom(pd, &envelope)

	case *pb.Envelope_JoinRoom:
		return rpc.handleJoinRoom(pd, &envelope, msg)

	case *pb.Envelope_LeaveRoom:
		return rpc.handleLeaveRoom(pd, &envelope)

	case *pb.Envelope_UpdateParticipantLocation:
		return rpc.handleUpdateParticipantLocation(pd, &envelope, msg)

	case *pb.Envelope_Follow:
		return rpc.handleFollow(pd, &envelope, msg)

	case *pb.Envelope_Unfollow:
		return rpc.handleUnfollow(pd, &envelope)

	case *pb.Envelope_ShareProject:
		return rpc.handleShareProject(pd, &envelope, msg)

	case *pb.Envelope_JoinProject:
		return rpc.handleJoinProject(pd, &envelope, msg)

	case *pb.Envelope_UnshareProject:
		return rpc.handleUnshareProject(pd, &envelope, msg)

	case *pb.Envelope_LeaveProject:
		return rpc.handleLeaveProject(pd, &envelope, msg)

	case *pb.Envelope_UpdateProject:
		return rpc.handleUpdateProject(pd, &envelope, msg)

	case *pb.Envelope_CreateBufferForPeer:
		return rpc.handleCreateBufferForPeer(pd, &envelope, msg)

	case *pb.Envelope_UpdateBuffer:
		return rpc.handleUpdateBuffer(pd, &envelope, msg)

	case *pb.Envelope_OpenBufferByPath:
		return rpc.handleOpenBufferByPath(pd, &envelope, msg)

	case *pb.Envelope_OpenBufferById:
		return rpc.handleOpenBufferById(pd, &envelope, msg)

	case *pb.Envelope_OpenNewBuffer:
		return rpc.handleOpenNewBuffer(pd, &envelope, msg)

	case *pb.Envelope_SaveBuffer:
		return rpc.handleSaveBuffer(pd, &envelope, msg)

	case *pb.Envelope_CloseBuffer:
		return rpc.handleCloseBuffer(pd, &envelope, msg)

	case *pb.Envelope_UpdateBufferFile:
		return rpc.handleUpdateBufferFile(pd, &envelope, msg)

	case *pb.Envelope_ReloadBuffers:
		return rpc.handleReloadBuffers(pd, &envelope, msg)

	case *pb.Envelope_SynchronizeBuffers:
		return rpc.handleSynchronizeBuffers(pd, &envelope, msg)

	case *pb.Envelope_AddProjectCollaborator:
		return rpc.handleAddProjectCollaborator(pd, &envelope, msg)

	case *pb.Envelope_RemoveProjectCollaborator:
		return rpc.handleRemoveProjectCollaborator(pd, &envelope, msg)

	case *pb.Envelope_FuzzySearchUsers:
		ru := rpc.users.Values()
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: ru,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SubscribeToChannels:
		rpc.sendUserChannels(uint64(pd.userId))
		channels := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		if err := pd.SendProtobuf(&channels); err != nil {
			return err
		}

	case *pb.Envelope_CreateChannel:
		ccr := msg.CreateChannel
		channel := &pb.Channel{
			Id:         *proto.Uint64(utils.StringToUInt64Hash(ccr.Name)),
			Name:       ccr.Name,
			Visibility: pb.ChannelVisibility_Public,
		}

		rpc.channels.Transaction(func(m map[uint64]*pb.Channel) map[uint64]*pb.Channel {
			if _, ok := m[channel.Id]; !ok {
				m[channel.Id] = channel
			}
			return m
		})
		rpc.channelMembers.Set(channel.Id, []*pb.ChannelMember{
			{UserId: uint64(pd.userId), Kind: pb.ChannelMember_Member, Role: pb.ChannelRole_Admin},
		})

		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		rpc.sendUserChannels(uint64(pd.userId))

		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_CreateChannelResponse{
				CreateChannelResponse: &pb.CreateChannelResponse{
					Channel: channel,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_JoinChannel:
		jc := msg.JoinChannel
		channel := rpc.channels.Get(jc.ChannelId)
		if channel == nil {
			resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
			if err := pd.SendProtobuf(&resp); err != nil {
				return err
			}
			break
		}
		role := pb.ChannelRole_Guest
		rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
			members := m[jc.ChannelId]
			found := false
			for _, mem := range members {
				if mem.UserId == uint64(pd.userId) {
					found = true
					if mem.Kind == pb.ChannelMember_Member {
						role = mem.Role
					}
				}
			}
			if !found {
				members = append(members, &pb.ChannelMember{UserId: uint64(pd.userId), Kind: pb.ChannelMember_Member, Role: role})
				m[jc.ChannelId] = members
			}
			return m
		})
		rpc.sendUserChannels(uint64(pd.userId))

		// A channel is backed by a room: joining the channel means joining the
		// room, which is how the other participants see each other.
		room := rpc.rooms.channelRoom(jc.ChannelId)
		room.join(uint64(pd.userId), pd.peerId)
		for _, member := range rpc.channelMembers.Get(jc.ChannelId) {
			if member.Kind == pb.ChannelMember_Member {
				room.join(member.UserId, nil)
			}
		}

		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		rpc.broadcastRoomUpdated(room, pd.userId)

		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinRoomResponse{
				JoinRoomResponse: &pb.JoinRoomResponse{
					Room:      room.toProto(),
					ChannelId: proto.Uint64(jc.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_JoinChannelChat:
		jcc := msg.JoinChannelChat
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelChatResponse{
				JoinChannelChatResponse: &pb.JoinChannelChatResponse{
					Messages: rpc.channelMessages.Get(jcc.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMembers:
		gcm := msg.GetChannelMembers
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMembersResponse{
				GetChannelMembersResponse: &pb.GetChannelMembersResponse{
					Members: rpc.channelMembers.Get(gcm.ChannelId),
					Users:   rpc.usersForChannel(gcm.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_InviteChannelMember:
		req := msg.InviteChannelMember
		if !rpc.users.Exists(req.UserId) {
			name, _, _ := identityFor(req.UserId)
			rpc.users.Set(req.UserId, &pb.User{
				Id:          req.UserId,
				Username:    name,
				GithubLogin: name,
				AvatarUrl:   rpc.avatarURL(req.UserId),
			})
		}
		inviteeChannels := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels:           []*pb.Channel{rpc.channels.Get(req.ChannelId)},
					ChannelInvitations: []*pb.Channel{rpc.channels.Get(req.ChannelId)},
				},
			},
		}
		inviteePD := NewProtoDispatcher(rpc, int(req.UserId))
		if err := inviteePD.SendProtobuf(&inviteeChannels); err != nil {
			log.Errorf("failed to notify invitee %v: %v", req.UserId, err)
		}
		rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
			members := m[req.ChannelId]
			found := false
			for _, mem := range members {
				if mem.UserId == req.UserId {
					found = true
				}
			}
			if !found {
				members = append(members, &pb.ChannelMember{UserId: req.UserId, Kind: pb.ChannelMember_Invitee, Role: req.Role})
				m[req.ChannelId] = members
			}
			return m
		})
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMessages:
		gcm := msg.GetChannelMessages
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMessagesResponse{
				GetChannelMessagesResponse: &pb.GetChannelMessagesResponse{
					Messages: rpc.channelMessages.Get(gcm.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RenameChannel:
		rc := msg.RenameChannel
		rpc.channels.Transaction(func(m map[uint64]*pb.Channel) map[uint64]*pb.Channel {
			if ch, ok := m[rc.ChannelId]; ok {
				ch.Name = rc.Name
				m[rc.ChannelId] = ch
			}
			return m
		})
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_RenameChannelResponse{
				RenameChannelResponse: &pb.RenameChannelResponse{
					Channel: rpc.channels.Get(rc.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_DeleteChannel:
		dc := msg.DeleteChannel
		rpc.channels.Delete(dc.ChannelId)
		rpc.channelMessages.Delete(dc.ChannelId)
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)

	case *pb.Envelope_RejoinChannelBuffers:
		rcb := msg.RejoinChannelBuffers
		rejoined := []*pb.RejoinedChannelBuffer{}
		for _, bv := range rcb.Buffers {
			state := rpc.channelBuffers.Get(bv.ChannelId)
			if state == nil {
				state = newChannelBufferState()
				rpc.channelBuffers.Set(bv.ChannelId, state)
			}
			state.addCollaborator(uint64(pd.userId), pd.peerId)
			_, _, ops, _ := state.snapshot()
			rejoined = append(rejoined, &pb.RejoinedChannelBuffer{
				ChannelId:     bv.ChannelId,
				Operations:    ops,
				Collaborators: state.collaboratorList(),
			})
		}
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_RejoinChannelBuffersResponse{
				RejoinChannelBuffersResponse: &pb.RejoinChannelBuffersResponse{
					Buffers: rejoined,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RespondToChannelInvite:
		ric := msg.RespondToChannelInvite
		if ric.Accept {
			rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
				members := m[ric.ChannelId]
				found := false
				for _, mem := range members {
					if mem.UserId == uint64(pd.userId) {
						mem.Kind = pb.ChannelMember_Member
						found = true
					}
				}
				if !found {
					members = append(members, &pb.ChannelMember{UserId: uint64(pd.userId), Kind: pb.ChannelMember_Member, Role: pb.ChannelRole_Member})
					m[ric.ChannelId] = members
				}
				return m
			})
			rpc.sendUserChannels(uint64(pd.userId))
		} else {
			rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
				members := []*pb.ChannelMember{}
				for _, mem := range m[ric.ChannelId] {
					if mem.UserId != uint64(pd.userId) {
						members = append(members, mem)
					}
				}
				m[ric.ChannelId] = members
				return m
			})
		}
		// The client keeps the invitation in its list until the server tells it
		// that the invitation was resolved.
		resolved := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels:                 rpc.channels.Values(),
					RemoveChannelInvitations: []uint64{ric.ChannelId},
				},
			},
		}
		if err := pd.SendProtobuf(&resolved); err != nil {
			return err
		}
		rpc.sendUserChannels(uint64(pd.userId))

		broadcast := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&broadcast, pd.userId)

		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_RemoveChannelMember:
		rmc := msg.RemoveChannelMember
		rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
			members := []*pb.ChannelMember{}
			for _, mem := range m[rmc.ChannelId] {
				if mem.UserId != rmc.UserId {
					members = append(members, mem)
				}
			}
			m[rmc.ChannelId] = members
			return m
		})
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SetChannelMemberRole:
		smr := msg.SetChannelMemberRole
		rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
			for _, mem := range m[smr.ChannelId] {
				if mem.UserId == smr.UserId {
					mem.Role = smr.Role
				}
			}
			return m
		})
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SetChannelVisibility:
		scv := msg.SetChannelVisibility
		rpc.channels.Transaction(func(m map[uint64]*pb.Channel) map[uint64]*pb.Channel {
			if ch, ok := m[scv.ChannelId]; ok {
				ch.Visibility = scv.Visibility
				m[scv.ChannelId] = ch
			}
			return m
		})
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_MoveChannel:
		mc := msg.MoveChannel
		rpc.channels.Transaction(func(m map[uint64]*pb.Channel) map[uint64]*pb.Channel {
			if ch, ok := m[mc.ChannelId]; ok {
				if mc.To != 0 {
					ch.ParentPath = append(ch.ParentPath, mc.To)
				}
				m[mc.ChannelId] = ch
			}
			return m
		})
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_ReorderChannel:
		rc := msg.ReorderChannel
		rpc.channels.Transaction(func(m map[uint64]*pb.Channel) map[uint64]*pb.Channel {
			if ch, ok := m[rc.ChannelId]; ok {
				if rc.Direction == 1 {
					ch.ChannelOrder++
				} else {
					ch.ChannelOrder--
				}
				m[rc.ChannelId] = ch
			}
			return m
		})
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: rpc.channelsUpdate(),
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_UpdateChannelBuffer:
		ucb := msg.UpdateChannelBuffer
		state := rpc.channelBuffers.Get(ucb.ChannelId)
		if state == nil {
			state = newChannelBufferState()
			rpc.channelBuffers.Set(ucb.ChannelId, state)
		}
		state.recordOperations(ucb.Operations)

		broadcast := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannelBuffer{
				UpdateChannelBuffer: ucb,
			},
		}
		rpc.broadcastToChannelCollaborators(ucb.ChannelId, &broadcast, pd.userId)

	case *pb.Envelope_LeaveChannelBuffer:
		lcb := msg.LeaveChannelBuffer
		state := rpc.channelBuffers.Get(lcb.ChannelId)
		if state != nil {
			state.removeCollaborator(uint64(pd.userId))
			updateCollabs := pb.Envelope{
				Id: pd.NextId(),
				Payload: &pb.Envelope_UpdateChannelBufferCollaborators{
					UpdateChannelBufferCollaborators: &pb.UpdateChannelBufferCollaborators{
						ChannelId:     lcb.ChannelId,
						Collaborators: state.collaboratorList(),
					},
				},
			}
			rpc.broadcastToChannelCollaborators(lcb.ChannelId, &updateCollabs, pd.userId)
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload:      &pb.Envelope_Ack{Ack: &pb.Ack{}},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_UpdateChannelBufferCollaborators:
		log.Debug("Envelope_UpdateChannelBufferCollaborators")

	case *pb.Envelope_AckBufferOperation:
		log.Debug("Envelope_AckBufferOperation")

	default:
		log.Infof("Received unmapped WebSocket message type=%T id=%v respondingTo=%v", envelope.Payload, envelope.Id, envelope.RespondingTo)
	}
	return nil
}

func (rpc *RpcHandler) generateWebSocketKey() string {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		log.Fatal("Failed to generate random key: ", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func (rpc *RpcHandler) HandleRequest(c *gin.Context) {
	upgrader := websocket.Upgrader{} // use default options
	c.Request.Header.Add("Upgrade", "websocket")
	c.Request.Header.Add("Connection", "upgrade")
	c.Request.Header.Add("Sec-WebSocket-Protocol", "chat")
	c.Request.Header.Add("Sec-WebSocket-Version", "13")
	c.Request.Header.Add("Sec-WebSocket-Key", rpc.generateWebSocketKey())

	auth := c.Request.Header.Get("Authorization")
	if len(strings.Split(auth, " ")) != 2 {
		log.Error("failed to get stuff from auth header")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stuff from auth header"})
		return
	}

	// NOTE: The other part of the map ([1]) is the decrypted data from the crypto challenge.
	connIdStr := strings.Split(auth, " ")[0]
	rawUserId, err := strconv.ParseUint(connIdStr, 10, 64)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	userId := int(CanonicalUserID(rawUserId))

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
		return
	}
	conn.SetReadLimit(WEBSOCKET_READ_LIMIT)

	// Every connection gets its own id: Zed addresses peers by connection id,
	// so a user may have several connections open at the same time.
	connId := rpc.nextConnectionId.Increment().Value()
	peerId := &pb.PeerId{OwnerId: 0, Id: connId}
	connection := &connection{
		id:     connId,
		userId: uint64(userId),
		peerId: peerId,
		socket: conn,
	}
	rpc.connections.Set(connId, connection)
	rpc.connectionsByPeer.Set(keyForPeerId(peerId), connId)
	rpc.userConnections.Transaction(func(m map[uint64][]uint32) map[uint64][]uint32 {
		m[uint64(userId)] = append(m[uint64(userId)], connId)
		return m
	})

	// TODO: Transaction
	if !rpc.users.Exists(uint64(userId)) {
		name, _, _ := identityFor(uint64(userId))
		u := &pb.User{
			Id:          uint64(userId),
			Username:    name,
			GithubLogin: name,
			AvatarUrl:   rpc.avatarURL(uint64(userId)),
		}
		rpc.users.Set(uint64(userId), u)
		log.Debugf("added a user, current users: %v", len(rpc.users.Map()))
	}
	pd := NewProtoDispatcherForConnection(rpc, connId)
	go rpc.handleMessages(pd)
	go rpc.keepAlive(connId)
	if err := pd.SendHello(); err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

// latestConnectionId returns the id of the most recently opened connection of
// a user.
func (rpc *RpcHandler) latestConnectionId(userId uint64) uint32 {
	connIds := rpc.userConnections.Get(userId)
	if len(connIds) == 0 {
		return 0
	}
	return connIds[len(connIds)-1]
}

// keepAlive sends websocket pings to the client. Zed disconnects a collab
// connection when it does not receive any message for RECEIVE_TIMEOUT
// (10 seconds), so the server has to keep the connection alive.
func (rpc *RpcHandler) keepAlive(connId uint32) {
	ticker := time.NewTicker(KEEPALIVE_INTERVAL)
	defer ticker.Stop()
	for range ticker.C {
		conn := rpc.connections.Get(connId)
		if conn == nil {
			return
		}
		err := conn.writeControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second))
		if err != nil {
			log.Debugf("keepalive failed for connection %v: %v", connId, err)
			return
		}
	}
}
