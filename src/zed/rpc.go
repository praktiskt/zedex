package zed

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
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

var (
	USERS            = utils.NewConcurrentMap[uint64, string]()
	CHANNEL_MEMBERS  = utils.NewConcurrentMap[uint64, []*pb.ChannelMember]()
	CHANNEL_MESSAGES = utils.NewConcurrentMap[uint64, []*pb.ChannelMessage]()
	NAMES            = namegenerator.NewGenerator()
)

type RpcHandler struct {
	websocket *websocket.Conn
}

func (rpc *RpcHandler) CompressMsg(b []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel((ZSTD_COMPRESSION_LEVEL)))
	if err != nil {
		return []byte{}, err
	}
	return encoder.EncodeAll(b, make([]byte, 0, len(b))), nil
}

func (rpc *RpcHandler) SendMessage(b []byte) error {
	bb, err := rpc.CompressMsg(b)
	if err != nil {
		return err
	}
	if err := rpc.websocket.WriteMessage(websocket.BinaryMessage, bb); err != nil {
		return err
	}
	return nil
}

func (rpc *RpcHandler) SendProtobuf(protobuf protoreflect.ProtoMessage) error {
	log.Infof("sending proto %#v", protobuf)
	b, err := proto.Marshal(protobuf)
	if err != nil {
		return err
	}
	return rpc.SendMessage(b)
}

func (rpc *RpcHandler) SendHello() error {
	helloMsg := &pb.Hello{
		PeerId: &pb.PeerId{Id: 1},
	}
	envelope := pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_Hello{
			Hello: helloMsg,
		},
	}
	return rpc.SendProtobuf(&envelope)
}

func (rpc *RpcHandler) handleMessages() {
	for {
		_, message, err := rpc.websocket.ReadMessage()
		if err != nil {
			log.Errorf("failed to receive message: %v", err)
			return
		}
		rpc.handleMessage(message)
	}
}

func (rpc *RpcHandler) handleMessage(message []byte) error {
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
				USERS.Set(uid, NAMES.Generate())
			}
			u := pb.User{
				Id:          uid,
				GithubLogin: USERS.Get(uid),
			}
			ru = append(ru, &u)
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: ru,
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetPrivateUserInfo:
		log.Debugf("Received get private users message: %v", msg)
		resp := pb.Envelope{
			RespondingTo:     &envelope.Id,
			OriginalSenderId: &pb.PeerId{Id: 2},
			Payload: &pb.Envelope_GetPrivateUserInfoResponse{
				GetPrivateUserInfoResponse: &pb.GetPrivateUserInfoResponse{
					MetricsId:     "123",
					Staff:         true,
					Flags:         []string{"zed-pro", "notebooks", "debugger", "llm-closed-beta", "thread-auto-capture"},
					AcceptedTosAt: proto.Uint64(1),
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

		resp = pb.Envelope{
			// RespondingTo:     &envelope.Id,
			// OriginalSenderId: &pb.PeerId{Id: 1},
			Payload: &pb.Envelope_AddNotification{
				AddNotification: &pb.AddNotification{
					Notification: &pb.Notification{
						Id:        1,
						Timestamp: uint64(time.Now().Unix()),
						Kind:      `ChannelMessageMention`,
						IsRead:    false,
						Content:   `{"sender_id": 1, "entity_id": 1, "channel_id": 1}`,
						Response:  proto.Bool(false),
					},
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMessagesById:
		ids := envelope.Payload.(*pb.Envelope_GetChannelMessagesById).GetChannelMessagesById.MessageIds
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
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SendChannelMessage:
		scm := envelope.Payload.(*pb.Envelope_SendChannelMessage).SendChannelMessage
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
		if err := rpc.SendProtobuf(&resp); err != nil {
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
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetNotifications:
		// TODO: Implement
		log.Debugf("Received GetNotifications message: %v", msg)

	case *pb.Envelope_FuzzySearchUsers:
		ru := []*pb.User{}
		for uid, name := range USERS.Map() {
			ru = append(ru, &pb.User{Id: uid, GithubLogin: name})
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: ru,
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
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
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_SubscribeToChannels:
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_SubscribeToChannels{
				SubscribeToChannels: &pb.SubscribeToChannels{},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_CreateChannel:
		ccr := envelope.Payload.(*pb.Envelope_CreateChannel)
		channel := pb.Channel{
			Id:         *proto.Uint64(utils.StringToUInt64Hash(ccr.CreateChannel.Name)),
			Name:       ccr.CreateChannel.Name,
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
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_JoinChannelChat:
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_JoinChannelChatResponse{
				JoinChannelChatResponse: &pb.JoinChannelChatResponse{
					Messages: []*pb.ChannelMessage{
						{Timestamp: uint64(time.Now().Unix()), Id: 1, Body: "Hello", SenderId: 1, Nonce: &pb.Nonce{UpperHalf: 0, LowerHalf: 2}},
					},
					Done: true,
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetChannelMembers:
		gcm := envelope.Payload.(*pb.Envelope_GetChannelMembers).GetChannelMembers
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_GetChannelMembersResponse{
				GetChannelMembersResponse: &pb.GetChannelMembersResponse{
					Members: CHANNEL_MEMBERS.Get(gcm.ChannelId),
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_InviteChannelMember:
		req := envelope.Payload.(*pb.Envelope_InviteChannelMember).InviteChannelMember
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
		// if err := rpc.SendProtobuf(&resp); err != nil {
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

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
		return
	}
	rpc.websocket = conn
	rpc.websocket.SetReadLimit(WEBSOCKET_READ_LIMIT)
	go rpc.handleMessages()
	if err := rpc.SendHello(); err != nil {
		log.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}
