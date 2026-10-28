package zed

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"encoding/base64"
	"net/http"
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

// socket wraps a websocket connection with a write mutex. Gorilla websocket
// does not allow concurrent writers, and zedex writes to a socket from
// several goroutines (message handlers, broadcasts).
type socket struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *socket) write(msgType int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(msgType, data)
}

func (s *socket) writeCompressed(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

type RpcHandler struct {
	sockets         utils.ConcurrentMap[int, *socket]
	users           utils.ConcurrentMap[uint64, *pb.User]
	channels        utils.ConcurrentMap[uint64, *pb.Channel]
	channelMembers  utils.ConcurrentMap[uint64, []*pb.ChannelMember]
	channelMessages utils.ConcurrentMap[uint64, []*pb.ChannelMessage]
	channelBuffers  utils.ConcurrentMap[uint64, *channelBufferState]
	id              utils.ConcurrentCounter[uint32]
	nameGenerator   namegenerator.NameGenerator
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
		sockets:         utils.NewConcurrentMap[int, *socket](),
		users:           utils.NewConcurrentMap[uint64, *pb.User](),
		channels:        utils.NewConcurrentMap[uint64, *pb.Channel](),
		channelMembers:  utils.NewConcurrentMap[uint64, []*pb.ChannelMember](),
		channelMessages: utils.NewConcurrentMap[uint64, []*pb.ChannelMessage](),
		channelBuffers:  utils.NewConcurrentMap[uint64, *channelBufferState](),
		id:              utils.NewConcurrentCounter[uint32](),
		nameGenerator:   namegenerator.NewGenerator(),
	}
}

type ProtoDispatcher struct {
	rpc    *RpcHandler
	userId int
	peerId *pb.PeerId
}

func NewProtoDispatcher(rpc *RpcHandler, userId int) *ProtoDispatcher {
	return &ProtoDispatcher{
		rpc:    rpc,
		userId: userId,
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
	if err := pd.rpc.sockets.Get(pd.userId).writeCompressed(bb); err != nil {
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
	envelope := pb.Envelope{
		Payload: &pb.Envelope_Hello{
			Hello: &pb.Hello{
				PeerId: &pb.PeerId{Id: uint32(pd.userId)},
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



func (rpc *RpcHandler) broadcastEnvelope(env *pb.Envelope, except int) {
	for _, uid := range rpc.sockets.Keys() {
		if uid == except {
			continue
		}
		pd := NewProtoDispatcher(rpc, uid)
		if err := pd.SendProtobuf(env); err != nil {
			log.Errorf("broadcast failed for user %v: %v", uid, err)
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
		if !rpc.sockets.Exists(int(uid)) {
			continue
		}
		pd := NewProtoDispatcher(rpc, int(uid))
		if err := pd.SendProtobuf(env); err != nil {
			log.Errorf("broadcast failed for user %v: %v", uid, err)
		}
	}
}



func (rpc *RpcHandler) usersForChannel(channelId uint64) []*pb.User {
	members := rpc.channelMembers.Get(channelId)
	users := []*pb.User{}
	for _, m := range members {
		u := rpc.users.Get(m.UserId)
		if u == nil {
			u = &pb.User{
				Id:          m.UserId,
				Username:    fmt.Sprintf("user_%d", m.UserId),
				GithubLogin: fmt.Sprintf("user_%d", m.UserId),
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

func (rpc *RpcHandler) handleMessages(pd *ProtoDispatcher) {
	sock := rpc.sockets.Get(pd.userId)
	conn := sock.conn
	for {
		// Read via NextReader + io.Copy instead of ReadMessage: gorilla's
		// ReadMessage uses io.ReadAll, which panics when a frame is larger
		// than the connection's internal read buffer.
		messageType, reader, err := conn.NextReader()
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

	switch msg := envelope.Payload.(type) {
	case *pb.Envelope_Hello:
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetUsers:
		gu := envelope.Payload.(*pb.Envelope_GetUsers)
		ru := []*pb.User{}
		for _, uid := range gu.GetUsers.UserIds {
			ru = append(ru, rpc.users.Get(uid))
		}
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
		log.Debug("Envelope_GetNotifications")

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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinRoomResponse{
				JoinRoomResponse: &pb.JoinRoomResponse{
					Room:      &pb.Room{Id: jc.ChannelId},
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
			rpc.users.Set(req.UserId, &pb.User{
				Id:          req.UserId,
				Username:    fmt.Sprintf("user_%d", req.UserId),
				GithubLogin: fmt.Sprintf("user_%d", req.UserId),
			})
		}
		inviteeChannels := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels:          []*pb.Channel{rpc.channels.Get(req.ChannelId)},
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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
		update := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannels{
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
			},
		}
		rpc.broadcastEnvelope(&update, pd.userId)
		rpc.sendUserChannels(uint64(pd.userId))
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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
				UpdateChannels: &pb.UpdateChannels{
					Channels: rpc.channels.Values(),
				},
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
		log.Infof("Received unmapped WebSocket message base64: %v", base64.StdEncoding.EncodeToString(message))
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
	rpc.sockets.Set(userId, &socket{conn: conn})
	rpc.sockets.Get(userId).conn.SetReadLimit(WEBSOCKET_READ_LIMIT)

	// TODO: Transaction
	if !rpc.users.Exists(uint64(userId)) {
		name := rpc.nameGenerator.Generate()
		u := &pb.User{
			Id:          uint64(userId),
			Username:    name,
			GithubLogin: name,
		}
		rpc.users.Set(uint64(userId), u)
		log.Debugf("added a user, current users: %v", len(rpc.users.Map()))
	}
	pd := NewProtoDispatcher(rpc, userId)
	go rpc.handleMessages(pd)
	go rpc.keepAlive(userId)
	if err := pd.SendHello(); err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

// keepAlive sends websocket pings to the client. Zed disconnects a collab
// connection when it does not receive any message for RECEIVE_TIMEOUT
// (10 seconds), so the server has to keep the connection alive.
func (rpc *RpcHandler) keepAlive(userId int) {
	ticker := time.NewTicker(KEEPALIVE_INTERVAL)
	defer ticker.Stop()
	for range ticker.C {
		sock := rpc.sockets.Get(userId)
		if sock == nil {
			return
		}
		sock.mu.Lock()
		err := sock.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second))
		sock.mu.Unlock()
		if err != nil {
			log.Debugf("keepalive failed for user %v: %v", userId, err)
			return
		}
	}
}
