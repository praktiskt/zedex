package zed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"zedex/pb"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type Controller struct {
	zed       Client
	localMode bool
}

func NewController(localMode bool, zedClient Client) Controller {
	return Controller{
		zed:       zedClient,
		localMode: localMode,
	}
}

func (co *Controller) Extensions(c *gin.Context) {
	var extensions Extensions
	var err error

	if co.localMode {
		extensionsFile := path.Join(co.zed.extensionsLocalDir, "extensions.json")
		extensions, err = co.zed.LoadExtensionIndex(extensionsFile)
	} else {
		extensions, err = co.zed.GetExtensionsIndex()
	}

	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	maxSchemaVersion := c.DefaultQuery("max_schema_version", "100")
	maxSchemaVersionInt, err := strconv.Atoi(maxSchemaVersion)
	if err != nil {
		c.JSON(400, gin.H{
			"error":   "Bad Request",
			"message": "max_schema_version must be an integer",
		})
		return
	}

	extensions = extensions.Filter(func(e Extension) bool {
		return e.SchemaVersion <= maxSchemaVersionInt
	})

	if filter := c.DefaultQuery("filter", ""); filter != "" {
		extensions = extensions.Filter(func(e Extension) bool {
			return strings.Contains(strings.ToLower(e.AsJsonStr()), strings.ToLower(filter))
		})
	}

	if filter := c.DefaultQuery("provides", ""); filter != "" {
		extensions = extensions.FilterByProvides(filter)
	}

	c.JSON(200, extensions.AsWrapped())
}

func (co *Controller) DownloadExtension(c *gin.Context) {
	id := c.Param("id")

	// TODO: Do we care about version?
	// minSchemaVersion := c.DefaultQuery("min_schema_version", "0")
	// minSchemaVersionInt, err := strconv.Atoi(minSchemaVersion)
	// if err != nil {
	// 	c.JSON(400, gin.H{
	// 		"error":   "Bad Request",
	// 		"message": "min_schema_version must be an integer",
	// 	})
	// 	return
	// }
	// maxSchemaVersion := c.Query("max_schema_version")
	// minWasmApiVersion := c.Query("min_wasm_api_version")
	// maxWasmApiVersion := c.Query("max_wasm_api_version")

	extension := Extension{ID: id}
	var bytes []byte
	var err error

	if co.localMode {
		bytes, err = co.zed.LoadExtensionArchive(extension)
	} else {
		bytes, err = co.zed.DownloadExtensionArchiveDefault(extension)
	}

	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.Data(200, "application/octet-stream", bytes)
}

func (co *Controller) LatestVersion(c *gin.Context) {
	var v Version
	var err error
	if co.localMode {
		versionFile := path.Join(co.zed.extensionsLocalDir, "latest_release.json")
		v, err = co.zed.LoadLatestZedVersionFromFile(versionFile)
	} else {
		v, err = co.zed.GetLatestZedVersion()
	}

	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(200, v)
}

func (co *Controller) LatestReleaseNotes(c *gin.Context) {
	var v ReleaseNotes
	var err error
	if co.localMode {
		versionFile := path.Join(co.zed.extensionsLocalDir, "latest_release_notes.json")
		v, err = co.zed.LoadReleaseNotes(versionFile)
	} else {
		v, err = co.zed.GetLatestReleaseNotes()
	}

	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(200, v)
}

// v1 is a reference to rusts rsa crate
func encryptStringV1(base64PublicKey, plaintext string) (string, error) {
	pubKeyBytes, err := base64.URLEncoding.DecodeString(base64PublicKey)
	if err != nil {
		return "", err
	}

	rsaPubKey, err := x509.ParsePKCS1PublicKey(pubKeyBytes)
	if err != nil {
		return "", err
	}

	encryptedBytes, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPubKey, []byte(plaintext), nil)
	if err != nil {
		return "", err
	}

	encryptedBase64 := base64.URLEncoding.EncodeToString(encryptedBytes)
	return encryptedBase64, nil
}

func randomToken() (string, error) {
	tokenBytes := make([]byte, 48)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return "", err
	}

	// Use base64.URLEncoding to get URL-safe encoding
	encodedToken := base64.URLEncoding.EncodeToString(tokenBytes)
	return encodedToken, nil
}

func (co *Controller) NativeAppSignin(c *gin.Context) {
	portStr := c.Query("native_app_port")
	pubKey := c.Query("native_app_public_key")

	// TODO: Figure out if its V1 or V0 for Zed. The rust crate tries both.
	enc, err := encryptStringV1(pubKey, "a")
	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	// user_id must be numeric, possibly a reference to github id
	// https://api.github.com/users/<user>
	host := fmt.Sprintf("http://localhost:%s/native_app_signin?user_id=1&access_token=%s", portStr, enc)
	logrus.Infof("sending request to %s", host)
	resp, err := http.Get(host)
	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": "Failed to get native app signin: " + string(b),
		})
		return
	}

	c.Redirect(302, "/native_app_signin_succeeded")
}

func (co *Controller) NativeAppSigninSucceeded(c *gin.Context) {
	c.Data(200,
		"text/html; charset=utf-8",
		[]byte(`<html>
		<body style="background-color: #1e1e2e; color: #ffffff; text-align: center; display: flex; justify-content: center; align-items: center">
			<p>You should now be signed into Zed. You can close this tab.</p>
		</body>
		</html>`,
		),
	)
}

func (co *Controller) HandleRpcRequest(c *gin.Context) {
	c.Redirect(301, "http://localhost:8080/some-url")
}

func zstdCompress(b []byte) []byte {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel((4)))
	if err != nil {
		panic(err)
	}
	compressedBytes := encoder.EncodeAll(b, make([]byte, 0, len(b)))
	return compressedBytes
}

// https://github.com/zed-industries/zed/blob/1e22faebc9f9c8da685a34b15c17f2bc2b418b26/crates/collab/src/rpc.rs#L1092
func (co *Controller) HandleWebSocketRequest(c *gin.Context) {
	protocolVersion := c.GetHeader("Protocol-Version")
	logrus.Info("protocolVersion", protocolVersion)
	if protocolVersion != "" {
		c.JSON(http.StatusUpgradeRequired, gin.H{"error": "client must be upgraded"})
		return
	}

	appVersionHeader := c.GetHeader("App-Version")
	logrus.Info("appVersionHeader:", appVersionHeader)
	if appVersionHeader != "" {
		c.JSON(http.StatusUpgradeRequired, gin.H{"error": "no version header found"})
		return
	}

	version, err := parseAppVersion(appVersionHeader)
	logrus.Infof("parsedAppVersion: %v, error: %v", version, err)
	if err != nil {
		c.JSON(http.StatusUpgradeRequired, gin.H{"error": "invalid version header"})
		return
	}

	// TODO
	if false { // !version.CanCollaborate() {
		c.JSON(http.StatusUpgradeRequired, gin.H{"error": "client must be upgraded"})
		return
	}

	socketAddress := c.ClientIP()

	upgrader := websocket.Upgrader{} // use default options
	c.Request.Header.Add("Upgrade", "websocket")
	c.Request.Header.Add("Connection", "upgrade")
	c.Request.Header.Add("Sec-WebSocket-Protocol", "chat")
	c.Request.Header.Add("Sec-WebSocket-Version", "13")
	c.Request.Header.Add("Sec-WebSocket-Key", "h3DWLuXsI9/GkTo+sIjyzw==")

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade connection"})
		return
	}
	// defer conn.Close()

	conn.SetReadLimit(1024 * 1024)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				logrus.Errorf("failed to receive message: %v", err)
				return
			}

			handleWebSocketMessage(conn, message)
		}
	}()

	helloMsg := &pb.Hello{
		PeerId: &pb.PeerId{Id: 1},
	}
	envelope := pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_Hello{
			Hello: helloMsg,
		},
	}
	data, err := proto.Marshal(&envelope)

	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel((4)))
	if err != nil {
		panic(err)
	}
	compressedBytes := encoder.EncodeAll(data, make([]byte, 0, len(data)))
	if err = conn.WriteMessage(websocket.BinaryMessage, compressedBytes); err != nil {
		logrus.Error(err)
	}

	server := &Server{}
	principal := &Principal{}
	countryCodeHeader := c.GetHeader("Cloudflare-Ip-Country")
	systemIdHeader := c.GetHeader("System-Id")
	handleConnection(server, principal, version, socketAddress, countryCodeHeader, systemIdHeader)
}

type Server struct{}

type Principal struct{}

func handleWebSocketMessage(conn *websocket.Conn, message []byte) {
	var envelope pb.Envelope
	err := proto.Unmarshal(message, &envelope)
	if err != nil {
		logrus.Errorf("failed to unmarshal message: %v", err)
		return
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
			Id:           2,
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_UsersResponse{
				UsersResponse: &pb.UsersResponse{
					Users: []*pb.User{user},
				},
			},
		}

		b, err := proto.Marshal(&resp)
		if err != nil {
			logrus.Error(err)
			return
		}
		conn.WriteMessage(websocket.BinaryMessage, zstdCompress(b))
	case *pb.Envelope_GetPrivateUserInfo:
		logrus.Infof("Received get private users message: %v", msg)
		resp := pb.Envelope{
			Id:               3,
			RespondingTo:     &envelope.Id,
			OriginalSenderId: &pb.PeerId{Id: 2},
			Payload: &pb.Envelope_GetPrivateUserInfoResponse{
				GetPrivateUserInfoResponse: &pb.GetPrivateUserInfoResponse{
					MetricsId: "123",
					Staff:     false,
					Flags:     []string{},
				},
			},
		}
		b, err := proto.Marshal(&resp)
		if err != nil {
			logrus.Error(err)
			return
		}

		conn.WriteMessage(websocket.BinaryMessage, zstdCompress(b))

	case *pb.Envelope_AcceptTermsOfService:
		logrus.Infof("Received TOS message: %v", msg)
		resp := pb.Envelope{
			Id:           1,
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_AcceptTermsOfServiceResponse{
				AcceptTermsOfServiceResponse: &pb.AcceptTermsOfServiceResponse{
					AcceptedTosAt: 1,
				},
			},
		}
		b, err := proto.Marshal(&resp)
		if err != nil {
			logrus.Error(err)
			return
		}
		conn.WriteMessage(websocket.BinaryMessage, zstdCompress(b))

	case *pb.Envelope_GetNotifications:
		// TODO: Implement
		logrus.Infof("Received GetNotifications message: %v", msg)
	default:
		logrus.Infof("Received unmapped message: %v", msg)
		logrus.Infof("Received WebSocket message: %v", string(message))
		logrus.Infof("Received WebSocket message base64: %v", base64.StdEncoding.EncodeToString(message))

	}
}

func handleConnection(server *Server, principal *Principal, version Version, socketAddress string, countryCodeHeader string, systemIdHeader string) {
	// TODO: Implement handling of WebSocket connections
	logrus.Infof("New WebSocket connection from %s", socketAddress)
}

func parseAppVersion(appVersionHeader string) (Version, error) {
	// TODO: Implement parsing of app version
	logrus.Infof("Parsing app version from header: %s", appVersionHeader)
	return Version{}, nil
}
