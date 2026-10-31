package zed

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"zedex/zed/auth"
	"zedex/zed/pb"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// testClient is a minimal client that speaks Zed's collab protocol over a
// websocket: zstd-compressed protobuf envelopes.
type testClient struct {
	conn *websocket.Conn
	t    *testing.T
}

func dialTestClient(t *testing.T, url string, userId uint64) *testClient {
	t.Helper()
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(url, http.Header(map[string][]string{
		"Authorization": {strconv.FormatUint(userId, 10) + " token"},
	}))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return &testClient{conn: conn, t: t}
}

func (c *testClient) send(env *pb.Envelope) {
	c.t.Helper()
	raw, err := proto.Marshal(env)
	require.NoError(c.t, err)
	compressed, err := CompressMsg(raw)
	require.NoError(c.t, err)
	require.NoError(c.t, c.conn.WriteMessage(websocket.BinaryMessage, compressed))
}

func (c *testClient) recv(timeout time.Duration) *pb.Envelope {
	c.t.Helper()
	if timeout <= 0 {
		return nil
	}
	require.NoError(c.t, c.conn.SetReadDeadline(time.Now().Add(timeout)))
	_, reader, err := c.conn.NextReader()
	if err != nil {
		return nil
	}
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := reader.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	decompressed, err := DecompressMsg(buf)
	if err != nil {
		return nil
	}
	var env pb.Envelope
	if err := proto.Unmarshal(decompressed, &env); err != nil {
		return nil
	}
	return &env
}

// waitForPayload reads envelopes until one matches the predicate.
func waitForPayload(t *testing.T, c *testClient, match func(*pb.Envelope) bool) *pb.Envelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		env := c.recv(time.Until(deadline))
		if env == nil {
			continue
		}
		if match(env) {
			return env
		}
	}
	return nil
}

func startTestRPCServer(t *testing.T, rpc *RpcHandler) string {
	t.Helper()
	return startTestRPCServerWithAuth(t, rpc, nil)
}

// startTestRPCServerWithAuth starts a collaboration server that checks the
// access tokens of its clients.
func startTestRPCServerWithAuth(t *testing.T, rpc *RpcHandler, manager *auth.Manager) string {
	t.Helper()
	api := NewAPI(Options{
		EnableLogin:        true,
		EnableReleases:     true,
		EnableReleaseNotes: true,
		Client:             NewZedClient(1),
		Auth:               manager,
	})
	api.rpc = rpc
	server := httptest.NewServer(api.Router())
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/handle-rpc"
}

func TestChannelBufferSync(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 1001)
	b := dialTestClient(t, server, 1002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	// A joins the channel buffer.
	a.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinChannelBuffer{JoinChannelBuffer: &pb.JoinChannelBuffer{ChannelId: 42}},
	})

	joinResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinChannelBufferResponse)
		return ok
	})
	require.NotNil(t, joinResp)
	jr := joinResp.Payload.(*pb.Envelope_JoinChannelBufferResponse).JoinChannelBufferResponse
	assert.Equal(t, uint64(42), jr.BufferId)
	assert.GreaterOrEqual(t, jr.ReplicaId, uint32(FIRST_COLLAB_ID))

	// B joins the same channel buffer and must receive the current state.
	b.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinChannelBuffer{JoinChannelBuffer: &pb.JoinChannelBuffer{ChannelId: 42}},
	})
	joinRespB := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinChannelBufferResponse)
		return ok
	})
	require.NotNil(t, joinRespB)

	// A types a character: it sends an edit operation for the buffer.
	op := &pb.Operation{
		Variant: &pb.Operation_Edit_{
			Edit: &pb.Operation_Edit{
				ReplicaId:        jr.ReplicaId,
				LamportTimestamp: 1,
				Version:          []*pb.VectorClockEntry{{ReplicaId: jr.ReplicaId, Timestamp: 1}},
				Ranges:           []*pb.Range{{Start: 0, End: 0}},
				NewText:          []string{"h"},
			},
		},
	}
	a.send(&pb.Envelope{
		Payload: &pb.Envelope_UpdateChannelBuffer{
			UpdateChannelBuffer: &pb.UpdateChannelBuffer{
				ChannelId:  42,
				Operations: []*pb.Operation{op},
			},
		},
	})

	// B must receive the relayed operation.
	relayed := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_UpdateChannelBuffer)
		return ok
	})
	require.NotNil(t, relayed, "B did not receive the relayed channel buffer operation")
	relayedOps := relayed.Payload.(*pb.Envelope_UpdateChannelBuffer).UpdateChannelBuffer.Operations
	require.Len(t, relayedOps, 1)
	assert.Equal(t, "h", relayedOps[0].GetEdit().NewText[0])

	// The server must have recorded the operation so that late joiners can
	// rebuild the buffer.
	state := rpc.channelBuffers.Get(42)
	require.NotNil(t, state)
	_, _, ops, _ := state.snapshot()
	require.Len(t, ops, 1)

	// A third client joining later must receive the operation history.
	c := dialTestClient(t, server, 1003)
	c.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinChannelBuffer{JoinChannelBuffer: &pb.JoinChannelBuffer{ChannelId: 42}},
	})
	joinRespC := waitForPayload(t, c, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinChannelBufferResponse)
		return ok
	})
	require.NotNil(t, joinRespC)
	lateOps := joinRespC.Payload.(*pb.Envelope_JoinChannelBufferResponse).JoinChannelBufferResponse.Operations
	require.Len(t, lateOps, 1)
	assert.Equal(t, "h", lateOps[0].GetEdit().NewText[0])
}
