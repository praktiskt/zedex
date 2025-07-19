package zed

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
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
	id              utils.ConcurrentCounter[uint32]
	nameGenerator   namegenerator.NameGenerator
}

func NewRpcHandler() RpcHandler {
	return RpcHandler{
		sockets:         utils.NewConcurrentMap[int, *websocket.Conn](),
		users:           utils.NewConcurrentMap[uint64, *pb.User](),
		channels:        utils.NewConcurrentMap[uint64, *pb.Channel](),
		channelMembers:  utils.NewConcurrentMap[uint64, []*pb.ChannelMember](),
		channelMessages: utils.NewConcurrentMap[uint64, []*pb.ChannelMessage](),
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

	case *pb.Envelope_GetPrivateUserInfo:
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetPrivateUserInfoResponse{
				GetPrivateUserInfoResponse: &pb.GetPrivateUserInfoResponse{
					MetricsId:     "123",
					Staff:         true,
					Flags:         []string{"zed-pro", "notebooks", "debugger", "llm-closed-beta", "thread-auto-capture"},
					AcceptedTosAt: proto.Uint64(1),
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
		log.Debug("Envelope_JoinChannelBuffer")
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelBuffer{
				JoinChannelBuffer: &pb.JoinChannelBuffer{
					ChannelId: msg.JoinChannelBuffer.ChannelId,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_AcceptTermsOfService:
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_AcceptTermsOfServiceResponse{
				AcceptTermsOfServiceResponse: &pb.AcceptTermsOfServiceResponse{},
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

	case *pb.Envelope_GetLlmToken:
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetLlmTokenResponse{
				GetLlmTokenResponse: &pb.GetLlmTokenResponse{
					Token: "abc123",
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SubscribeToChannels:
		log.Debug("Envelope_SubscribeToChannels")

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
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_InviteChannelMember:
		req := msg.InviteChannelMember
		rpc.channelMembers.Transaction(func(m map[uint64][]*pb.ChannelMember) map[uint64][]*pb.ChannelMember {
			m[req.UserId] = append(m[req.UserId], &pb.ChannelMember{UserId: req.UserId, Kind: pb.ChannelMember_Member, Role: pb.ChannelRole_Admin})
			return m
		})

	case *pb.Envelope_JoinChannel:
		// jc := envelope.Payload.(*pb.Envelope_JoinChannel).JoinChannel
		// resp := pb.Envelope{
		// 	RespondingTo: &envelope.Id,
		// 	Payload: &pb.Envelope_JoinChannelChatResponse{
		// 		JoinChannelChatResponse: &pb.JoinChannelChatResponse{},
		// 	},
		// }
		// if err := rpc.SendProtobuf(connId, &resp); err != nil {
		// 	return err
		// }
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
		u := &pb.User{
			Id:          uint64(userId),
			GithubLogin: rpc.nameGenerator.Generate(),
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
