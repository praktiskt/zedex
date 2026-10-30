package zed

import (
	"testing"

	"zedex/zed/pb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharedProjectIsVisibleInRoom verifies that a project shared inside a
// room shows up in the room state of the other participant, which is how Zed
// surfaces shared projects in the collaboration panel.
func TestSharedProjectIsVisibleInRoom(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 8001)
	b := dialTestClient(t, server, 8002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	// A creates a room (a call).
	a.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_CreateRoom{CreateRoom: &pb.CreateRoom{}},
	})
	createResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_CreateRoomResponse)
		return ok
	})
	require.NotNil(t, createResp)
	roomId := createResp.Payload.(*pb.Envelope_CreateRoomResponse).CreateRoomResponse.Room.Id
	require.NotZero(t, roomId)

	// B joins the room.
	b.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinRoom{JoinRoom: &pb.JoinRoom{Id: roomId}},
	})
	joinResp := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinRoomResponse)
		return ok
	})
	require.NotNil(t, joinResp)
	room := joinResp.Payload.(*pb.Envelope_JoinRoomResponse).JoinRoomResponse.Room
	require.Len(t, room.GetParticipants(), 2, "both participants should be in the room")

	// A shares a project into the room.
	a.send(&pb.Envelope{
		Id: 2,
		Payload: &pb.Envelope_ShareProject{
			ShareProject: &pb.ShareProject{
				RoomId:    roomId,
				Worktrees: []*pb.WorktreeMetadata{{Id: 1, RootName: "zedex"}},
			},
		},
	})
	shareResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_ShareProjectResponse)
		return ok
	})
	require.NotNil(t, shareResp)
	projectId := shareResp.Payload.(*pb.Envelope_ShareProjectResponse).ShareProjectResponse.ProjectId

	// B must see the shared project in the room state.
	roomUpdate := waitForPayload(t, b, func(e *pb.Envelope) bool {
		ru, ok := e.Payload.(*pb.Envelope_RoomUpdated)
		if !ok {
			return false
		}
		for _, p := range ru.RoomUpdated.GetRoom().GetParticipants() {
			for _, project := range p.GetProjects() {
				if project.GetId() == projectId {
					return true
				}
			}
		}
		return false
	})
	require.NotNil(t, roomUpdate, "B did not see the shared project in the room")

	participants := roomUpdate.Payload.(*pb.Envelope_RoomUpdated).RoomUpdated.GetRoom().GetParticipants()
	var sharedBy *pb.ParticipantProject
	for _, p := range participants {
		for _, project := range p.GetProjects() {
			if project.GetId() == projectId {
				sharedBy = project
			}
		}
	}
	require.NotNil(t, sharedBy)
	assert.Contains(t, sharedBy.GetWorktreeRootNames(), "zedex")
}

// TestChannelJoinShowsParticipants verifies that joining a channel makes the
// participants visible to the other clients, which is what the collaboration
// panel uses to show who is in a channel.
func TestChannelJoinShowsParticipants(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 8101)
	b := dialTestClient(t, server, 8102)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	// A creates a channel.
	a.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_CreateChannel{
			CreateChannel: &pb.CreateChannel{Name: "general"},
		},
	})
	createResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_CreateChannelResponse)
		return ok
	})
	require.NotNil(t, createResp)
	channelId := createResp.Payload.(*pb.Envelope_CreateChannelResponse).CreateChannelResponse.Channel.Id

	// B joins the channel.
	b.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinChannel{JoinChannel: &pb.JoinChannel{ChannelId: channelId}},
	})
	joinResp := waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinRoomResponse)
		return ok
	})
	require.NotNil(t, joinResp)

	// A must learn that B is in the channel.
	participants := waitForPayload(t, a, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateChannels)
		if !ok {
			return false
		}
		for _, entry := range uc.UpdateChannels.GetChannelParticipants() {
			if entry.GetChannelId() == channelId && len(entry.GetParticipantUserIds()) > 0 {
				return true
			}
		}
		return false
	})
	require.NotNil(t, participants, "A did not see B joining the channel")

	entries := participants.Payload.(*pb.Envelope_UpdateChannels).UpdateChannels.GetChannelParticipants()
	var found bool
	for _, entry := range entries {
		if entry.GetChannelId() == channelId {
			found = true
			assert.Contains(t, entry.GetParticipantUserIds(), uint64(8102))
		}
	}
	assert.True(t, found, "the channel participants did not include the channel")
}

// TestChannelParticipantsHaveUsernamesAndAvatars verifies that the users
// reported as channel participants carry a username and an avatar, so that
// Zed can render them.
func TestChannelParticipantsHaveUsernamesAndAvatars(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 8201)
	b := dialTestClient(t, server, 8202)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	a.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_CreateChannel{
			CreateChannel: &pb.CreateChannel{Name: "general"},
		},
	})
	createResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_CreateChannelResponse)
		return ok
	})
	require.NotNil(t, createResp)
	channelId := createResp.Payload.(*pb.Envelope_CreateChannelResponse).CreateChannelResponse.Channel.Id

	b.send(&pb.Envelope{
		Id:      1,
		Payload: &pb.Envelope_JoinChannel{JoinChannel: &pb.JoinChannel{ChannelId: channelId}},
	})
	require.NotNil(t, waitForPayload(t, b, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_JoinRoomResponse)
		return ok
	}))

	participants := waitForPayload(t, a, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateChannels)
		if !ok {
			return false
		}
		for _, entry := range uc.UpdateChannels.GetChannelParticipants() {
			if entry.GetChannelId() == channelId && len(entry.GetParticipantUserIds()) > 0 {
				return true
			}
		}
		return false
	})
	require.NotNil(t, participants)

	entries := participants.Payload.(*pb.Envelope_UpdateChannels).UpdateChannels.GetChannelParticipants()
	var userIds []uint64
	for _, entry := range entries {
		if entry.GetChannelId() == channelId {
			userIds = entry.GetParticipantUserIds()
		}
	}
	require.NotEmpty(t, userIds)

	a.send(&pb.Envelope{
		Id:      2,
		Payload: &pb.Envelope_GetUsers{GetUsers: &pb.GetUsers{UserIds: userIds}},
	})
	usersResp := waitForPayload(t, a, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_UsersResponse)
		return ok
	})
	require.NotNil(t, usersResp)
	users := usersResp.Payload.(*pb.Envelope_UsersResponse).UsersResponse.GetUsers()
	require.Len(t, users, len(userIds))

	for i := 1; i < len(users); i++ {
		assert.Less(t, users[i-1].GetId(), users[i].GetId(), "users must be sorted by id")
	}
	for _, u := range users {
		assert.NotEmpty(t, u.GetUsername(), "a user without a username is invisible in the UI")
		assert.NotEmpty(t, u.GetAvatarUrl(), "a user without an avatar is invisible in the UI")
	}
}
