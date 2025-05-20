package zed

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"zedex/zed/pb"

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
		user := &pb.User{
			Id:          1,
			GithubLogin: "anonymous",
			AvatarUrl:   "",
		}
		resp := pb.Envelope{
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: []*pb.User{user},
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_GetPrivateUserInfo:
		log.Debugf("Received get private users message: %v", msg)
		acceptTos := uint64(1)
		resp := pb.Envelope{
			RespondingTo:     &envelope.Id,
			OriginalSenderId: &pb.PeerId{Id: 2},
			Payload: &pb.Envelope_GetPrivateUserInfoResponse{
				GetPrivateUserInfoResponse: &pb.GetPrivateUserInfoResponse{
					MetricsId:     "123",
					Staff:         true,
					Flags:         []string{"zed-pro", "notebooks", "debugger", "llm-closed-beta", "thread-auto-capture"},
					AcceptedTosAt: &acceptTos,
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

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
