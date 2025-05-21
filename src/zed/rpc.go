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
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	ZSTD_COMPRESSION_LEVEL = 4
	WEBSOCKET_READ_LIMIT   = 1024 * 1024
)

var (
	USERS            = utils.NewConcurrentMap[uint64, *pb.User]()
	CHANNEL_MEMBERS  = utils.NewConcurrentMap[uint64, []*pb.ChannelMember]()
	CHANNEL_MESSAGES = utils.NewConcurrentMap[uint64, []*pb.ChannelMessage]()
	NAMES            = namegenerator.NewGenerator()
)

type RpcHandler struct {
	sockets         utils.ConcurrentMap[int, *websocket.Conn]
	users           utils.ConcurrentMap[uint64, *pb.User]
	channelMembers  utils.ConcurrentMap[uint64, []*pb.ChannelMember]
	channelMessages utils.ConcurrentMap[uint64, []*pb.ChannelMessage]
	nameGenerator   namegenerator.NameGenerator
}

func NewRpcHandler() RpcHandler {
	return RpcHandler{
		sockets:         utils.NewConcurrentMap[int, *websocket.Conn](),
		users:           utils.NewConcurrentMap[uint64, *pb.User](),
		channelMembers:  utils.NewConcurrentMap[uint64, []*pb.ChannelMember](),
		channelMessages: utils.NewConcurrentMap[uint64, []*pb.ChannelMessage](),
		nameGenerator:   namegenerator.NewGenerator(),
	}
}

type ProtoDispatcher struct {
	rpc    *RpcHandler
	userId int
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
	log.Infof("sending proto %#v", protobuf)
	b, err := proto.Marshal(protobuf)
	if err != nil {
		return err
	}
	return pd.SendMessage(b)
}

func (pd *ProtoDispatcher) SendHello() error {
	helloMsg := &pb.Hello{
		PeerId: &pb.PeerId{Id: 1},
	}
	envelope := pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_Hello{
			Hello: helloMsg,
		},
	}
	return pd.SendProtobuf(&envelope)
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

	switch msg := envelope.Payload.(type) {
	case *pb.Envelope_Hello:
		log.Debugf("Received hello message: %v", msg)
	case *pb.Envelope_GetUsers:
		log.Debugf("Received get users message: %v", msg)

		gu := envelope.Payload.(*pb.Envelope_GetUsers)
		ru := []*pb.User{}
		for _, uid := range gu.GetUsers.UserIds {
			if !USERS.Exists(uid) {
				u := &pb.User{
					Id:          uid,
					GithubLogin: NAMES.Generate(),
				}
				USERS.Set(uid, u)
			}
			ru = append(ru, USERS.Get(uid))
		}
		logrus.Debugf("current users: %#v", USERS.Map())
		resp := pb.Envelope{
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
			msgs = append(msgs, CHANNEL_MESSAGES.Get(id)...)
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMessagesResponse{
				GetChannelMessagesResponse: &pb.GetChannelMessagesResponse{
					Done:     true,
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

		CHANNEL_MESSAGES.Transaction(scm.ChannelId, func(m *utils.ConcurrentMap[uint64, []*pb.ChannelMessage]) {
			currentMessages := m.GetUnsafe(scm.ChannelId)
			currentMessages = append(currentMessages, channelMsg)
			m.SetUnsafe(scm.ChannelId, currentMessages)
		})

		resp := pb.Envelope{
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
		// acm := envelope.Payload.(*pb.Envelope_AckChannelMessage).AckChannelMessage
	case *pb.Envelope_LeaveChannelChat:
		log.Debug("Envelope_LeaveChannelChat")
		// acm := envelope.Payload.(*pb.Envelope_AckChannelMessage).AckChannelMessage

	case *pb.Envelope_AcceptTermsOfService:
		log.Debugf("Received TOS message: %v", msg)
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_AcceptTermsOfServiceResponse{
				AcceptTermsOfServiceResponse: &pb.AcceptTermsOfServiceResponse{},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetNotifications:
		// TODO: Implement
		log.Debugf("Received GetNotifications message: %v", msg)

	case *pb.Envelope_FuzzySearchUsers:
		ru := USERS.Values()
		resp := pb.Envelope{
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
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_SubscribeToChannels{
				SubscribeToChannels: &pb.SubscribeToChannels{},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_CreateChannel:
		ccr := msg.CreateChannel
		channel := pb.Channel{
			Id:         *proto.Uint64(utils.StringToUInt64Hash(ccr.Name)),
			Name:       ccr.Name,
			Visibility: pb.ChannelVisibility_Public,
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_CreateChannelResponse{
				CreateChannelResponse: &pb.CreateChannelResponse{
					Channel: &channel,
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_JoinChannelChat:
		jcc := msg.JoinChannelChat
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelChatResponse{
				JoinChannelChatResponse: &pb.JoinChannelChatResponse{
					Messages: CHANNEL_MESSAGES.Get(jcc.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMembers:
		gcm := msg.GetChannelMembers
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMembersResponse{
				GetChannelMembersResponse: &pb.GetChannelMembersResponse{
					Members: CHANNEL_MEMBERS.Get(gcm.ChannelId),
				},
			},
		}
		if err := pd.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_InviteChannelMember:
		req := msg.InviteChannelMember
		CHANNEL_MEMBERS.Transaction(req.UserId, func(m *utils.ConcurrentMap[uint64, []*pb.ChannelMember]) {
			currentMembers := m.GetUnsafe(req.ChannelId)
			currentMembers = append(currentMembers, &pb.ChannelMember{UserId: req.UserId, Kind: pb.ChannelMember_Member, Role: pb.ChannelRole_Admin})
			m.SetUnsafe(req.UserId, currentMembers)
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
	pd := NewProtoDispatcher(rpc, userId)
	go rpc.handleMessages(pd)
	if err := pd.SendHello(); err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}
