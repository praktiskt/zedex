package zed

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"

	"zedex/zed/pb"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	"github.com/sirupsen/logrus"
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

func (rpc *RpcHandler) CompressMsg(b []byte) []byte {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel((ZSTD_COMPRESSION_LEVEL)))
	if err != nil {
		panic(err) // TODO
	}
	return encoder.EncodeAll(b, make([]byte, 0, len(b)))
}

func (rpc *RpcHandler) SendMessage(b []byte) error {
	b = rpc.CompressMsg(b)
	if err := rpc.websocket.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return err
	}
	return nil
}

func (rpc *RpcHandler) SendProtobuf(protobuf protoreflect.ProtoMessage) error {
	logrus.Infof("sending proto %#v", protobuf)
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
			logrus.Errorf("failed to receive message: %v", err)
			return
		}
		rpc.handleMessage(message)
	}
}

func (rpc *RpcHandler) handleMessage(message []byte) error {
	var envelope pb.Envelope
	err := proto.Unmarshal(message, &envelope)
	if err != nil {
		logrus.Errorf("failed to unmarshal message: %v", err)
		return err
	}

	switch msg := envelope.Payload.(type) {
	case *pb.Envelope_Hello:
		logrus.Infof("Received hello message: %v", msg)
	case *pb.Envelope_GetUsers:
		logrus.Infof("Received get users message: %v", msg)
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
		logrus.Infof("Received get private users message: %v", msg)
		acceptTos := uint64(1)
		resp := pb.Envelope{
			RespondingTo:     &envelope.Id,
			OriginalSenderId: &pb.PeerId{Id: 2},
			Payload: &pb.Envelope_GetPrivateUserInfoResponse{
				GetPrivateUserInfoResponse: &pb.GetPrivateUserInfoResponse{
					MetricsId:     "123",
					Staff:         false,
					Flags:         []string{},
					AcceptedTosAt: &acceptTos,
				},
			},
		}
		if err := rpc.SendProtobuf(&resp); err != nil {
			return err
		}

	case *pb.Envelope_AcceptTermsOfService:
		logrus.Infof("Received TOS message: %v", msg)
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
		logrus.Infof("Received GetNotifications message: %v", msg)

	case *pb.Envelope_GetLlmToken:
		resp := pb.Envelope{
			Id:           1,
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
		logrus.Infof("Received unmapped message: %v", msg)
		logrus.Infof("Received WebSocket message: %v", string(message))
		logrus.Infof("Received WebSocket message base64: %v", base64.StdEncoding.EncodeToString(message))
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
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
		return
	}
	rpc.websocket = conn
	rpc.websocket.SetReadLimit(WEBSOCKET_READ_LIMIT)
	go rpc.handleMessages()
	if err := rpc.SendHello(); err != nil {
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}
