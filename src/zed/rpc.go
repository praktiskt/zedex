package zed

import (
	"crypto/rand"
	"fmt"
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
)

type RpcHandler struct {
	sockets         utils.ConcurrentMap[int, *websocket.Conn]
	users           utils.ConcurrentMap[uint64, *pb.User]
	channels        utils.ConcurrentMap[uint64, *pb.Channel]
	channelMembers  utils.ConcurrentMap[uint64, []*pb.ChannelMember]
	channelMessages utils.ConcurrentMap[uint64, []*pb.ChannelMessage]
	channelBuffers  utils.ConcurrentMap[uint64, *channelBufferState]
	id              utils.ConcurrentCounter[uint32]
	nameGenerator   namegenerator.NameGenerator
}



type channelBufferState struct {
	mu            sync.Mutex
	baseText      string
	epoch         uint64
	operations    []*pb.Operation
	version       []*pb.VectorClockEntry
	collaborators []uint64
}

func NewRpcHandler() RpcHandler {
	return RpcHandler{
		sockets:         utils.NewConcurrentMap[int, *websocket.Conn](),
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
	if err := pd.rpc.sockets.Get(pd.userId).WriteMessage(websocket.BinaryMessage, bb); err != nil {
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

func (rpc *RpcHandler) handleMessages(pd *ProtoDispatcher) {
	for {
		_, message, err := rpc.sockets.Get(pd.userId).ReadMessage()
		if err != nil {
			log.Errorf("failed to receive message: %v", err)
			return
		}
		rpc.handleMessage(pd, message)
	}
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
			state = &channelBufferState{baseText: "", epoch: 1}
			rpc.channelBuffers.Set(jcb.ChannelId, state)
		}
		state.mu.Lock()
		if !containsUint64(state.collaborators, uint64(pd.userId)) {
			state.collaborators = append(state.collaborators, uint64(pd.userId))
		}
		ops := append([]*pb.Operation{}, state.operations...)
		base := state.baseText
		epoch := state.epoch
		version := append([]*pb.VectorClockEntry{}, state.version...)
		collabs := append([]uint64{}, state.collaborators...)
		state.mu.Unlock()

		// notify other collaborators
		updateCollabs := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannelBufferCollaborators{
				UpdateChannelBufferCollaborators: &pb.UpdateChannelBufferCollaborators{
					ChannelId:     jcb.ChannelId,
					Collaborators: collabProtos(collabs),
				},
			},
		}
		rpc.broadcastEnvelope(&updateCollabs, pd.userId)

		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelBufferResponse{
				JoinChannelBufferResponse: &pb.JoinChannelBufferResponse{
					BufferId:      jcb.ChannelId,
					ReplicaId:     uint32(pd.userId),
					BaseText:      base,
					Operations:    ops,
					Epoch:         epoch,
					Collaborators: collabProtos(collabs),
				},
			},
		}
		_ = version
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
				state = &channelBufferState{baseText: "", epoch: 1}
				rpc.channelBuffers.Set(bv.ChannelId, state)
			}
			state.mu.Lock()
			ops := append([]*pb.Operation{}, state.operations...)
			collabs := append([]uint64{}, state.collaborators...)
			epoch := state.epoch
			state.mu.Unlock()
			rejoined = append(rejoined, &pb.RejoinedChannelBuffer{
				ChannelId:     bv.ChannelId,
				Operations:    ops,
				Collaborators: collabProtos(collabs),
			})
			_ = epoch
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
			state = &channelBufferState{baseText: "", epoch: 1}
			rpc.channelBuffers.Set(ucb.ChannelId, state)
		}
		state.mu.Lock()
		state.operations = append(state.operations, ucb.Operations...)
		state.epoch++
		state.mu.Unlock()

		broadcast := pb.Envelope{
			Id: pd.NextId(),
			Payload: &pb.Envelope_UpdateChannelBuffer{
				UpdateChannelBuffer: ucb,
			},
		}
		rpc.broadcastEnvelope(&broadcast, pd.userId)

	case *pb.Envelope_LeaveChannelBuffer:
		lcb := msg.LeaveChannelBuffer
		state := rpc.channelBuffers.Get(lcb.ChannelId)
		if state != nil {
			state.mu.Lock()
			filtered := []uint64{}
			for _, c := range state.collaborators {
				if c != uint64(pd.userId) {
					filtered = append(filtered, c)
				}
			}
			state.collaborators = filtered
			state.mu.Unlock()
			updateCollabs := pb.Envelope{
				Id: pd.NextId(),
				Payload: &pb.Envelope_UpdateChannelBufferCollaborators{
					UpdateChannelBufferCollaborators: &pb.UpdateChannelBufferCollaborators{
						ChannelId:     lcb.ChannelId,
						Collaborators: collabProtos(filtered),
					},
				},
			}
			rpc.broadcastEnvelope(&updateCollabs, pd.userId)
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload:      &pb.Envelope_Ack{},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_UpdateChannelBufferCollaborators:
		log.Debug("Envelope_UpdateChannelBufferCollaborators")

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
	userId, err := strconv.Atoi(connIdStr)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
		return
	}
	rpc.sockets.Set(userId, conn)
	rpc.sockets.Get(userId).SetReadLimit(WEBSOCKET_READ_LIMIT)

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
	if err := pd.SendHello(); err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}
