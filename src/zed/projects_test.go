package zed

import (
	"testing"
	"zedex/zed/pb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestSharedProjectForwarding verifies that a guest's project request is
// forwarded to the client hosting the project and that the response is
// relayed back to the guest.
func TestSharedProjectForwarding(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	host := dialTestClient(t, server, 2001)
	guest := dialTestClient(t, server, 2002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	host.send(hello)
	guest.send(hello)

	// The host shares a project.
	host.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_ShareProject{
			ShareProject: &pb.ShareProject{
				Worktrees: []*pb.WorktreeMetadata{{Id: 1, RootName: "zedex"}},
			},
		},
	})
	shareResp := waitForPayload(t, host, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_ShareProjectResponse)
		return ok
	})
	require.NotNil(t, shareResp)
	projectId := shareResp.Payload.(*pb.Envelope_ShareProjectResponse).ShareProjectResponse.ProjectId
	assert.NotZero(t, projectId)

	// The guest joins the project.
	guest.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_JoinProject{
			JoinProject: &pb.JoinProject{ProjectId: projectId},
		},
	})
	joinResp := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinProjectResponse)
		return ok
	})
	require.NotNil(t, joinResp)

	// The guest opens a buffer: the request must be forwarded to the host.
	guest.send(&pb.Envelope{
		Id: 2,
		Payload: &pb.Envelope_OpenBufferByPath{
			OpenBufferByPath: &pb.OpenBufferByPath{
				ProjectId:  projectId,
				WorktreeId: 1,
				Path:       "main.rs",
			},
		},
	})

	forwarded := waitForPayload(t, host, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_OpenBufferByPath)
		return ok
	})
	require.NotNil(t, forwarded, "host did not receive the forwarded request")

	// The host answers; the response must be relayed to the guest.
	host.send(&pb.Envelope{
		Id:           100,
		RespondingTo: proto.Uint32(forwarded.GetId()),
		Payload: &pb.Envelope_OpenBufferResponse{
			OpenBufferResponse: &pb.OpenBufferResponse{BufferId: 4242},
		},
	})

	relayed := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_OpenBufferResponse)
		return ok
	})
	require.NotNil(t, relayed, "guest did not receive the relayed response")
	assert.Equal(t, uint64(4242), relayed.Payload.(*pb.Envelope_OpenBufferResponse).OpenBufferResponse.BufferId)
}

// TestNotificationsBroadcast verifies that a notification added by one client
// is broadcast to the others and can be listed.
func TestNotificationsBroadcast(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 3001)
	b := dialTestClient(t, server, 3002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	a.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_AddNotification{
			AddNotification: &pb.AddNotification{
				Notification: &pb.Notification{Kind: "test", Content: "hello"},
			},
		},
	})

	broadcast := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_UpdateNotification)
		return ok
	})
	require.NotNil(t, broadcast, "notification was not broadcast")
	notification := broadcast.Payload.(*pb.Envelope_UpdateNotification).UpdateNotification.Notification
	assert.Equal(t, "hello", notification.Content)

	// The notification must also be listed by GetNotifications.
	b.send(&pb.Envelope{
		Id:      2,
		Payload: &pb.Envelope_GetNotifications{GetNotifications: &pb.GetNotifications{}},
	})
	list := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_GetNotificationsResponse)
		return ok
	})
	require.NotNil(t, list)
	notifications := list.Payload.(*pb.Envelope_GetNotificationsResponse).GetNotificationsResponse.Notifications
	require.Len(t, notifications, 1)
	assert.Equal(t, "hello", notifications[0].Content)
}

// TestPingResponse verifies the ping/pong handshake used by clients to check
// that the connection is alive.
func TestPingResponse(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	client := dialTestClient(t, server, 4001)
	client.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_Ping{Ping: &pb.Ping{}},
	})

	resp := waitForPayload(t, client, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_Ack)
		return ok
	})
	require.NotNil(t, resp, "no response to ping")
	require.NotNil(t, resp.GetRespondingTo())
}
