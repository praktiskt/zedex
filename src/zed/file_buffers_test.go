package zed

import (
	"testing"

	"zedex/zed/pb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestFileBufferGuestFlow walks through the flow a guest performs when
// opening a file in a project hosted by another client:
//
//  1. the guest asks the server to open the buffer,
//  2. the server forwards the request to the host,
//  3. the host answers with a buffer id,
//  4. the server relays that answer back to the guest,
//  5. the host sends the buffer state to the guest,
//  6. edits made by the host reach the guest.
func TestFileBufferGuestFlow(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	host := dialTestClient(t, server, 5001)
	guest := dialTestClient(t, server, 5002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	host.send(hello)
	guest.send(hello)

	// The guest's peer id: the host needs it to address the guest.
	guestPeerId := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_Hello)
		return ok
	})
	require.NotNil(t, guestPeerId)
	guestPeer := guestPeerId.Payload.(*pb.Envelope_Hello).Hello.GetPeerId()
	require.NotNil(t, guestPeer)

	// The host shares a project.
	host.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_ShareProject{
			ShareProject: &pb.ShareProject{
				Worktrees: []*pb.WorktreeMetadata{{Id: 7, RootName: "zedex"}},
			},
		},
	})
	shareResp := waitForPayload(t, host, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_ShareProjectResponse)
		return ok
	})
	require.NotNil(t, shareResp)
	projectId := shareResp.Payload.(*pb.Envelope_ShareProjectResponse).ShareProjectResponse.ProjectId

	// The guest joins the project and learns the host's peer id.
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
	collaborators := joinResp.Payload.(*pb.Envelope_JoinProjectResponse).JoinProjectResponse.Collaborators
	require.NotEmpty(t, collaborators)

	var hostPeer *pb.PeerId
	for _, c := range collaborators {
		if c.GetUserId() == 5001 {
			hostPeer = c.GetPeerId()
		}
	}
	require.NotNil(t, hostPeer, "the guest did not learn the host's peer id")

	// The guest opens a file: the request must reach the host.
	guest.send(&pb.Envelope{
		Id: 2,
		Payload: &pb.Envelope_OpenBufferByPath{
			OpenBufferByPath: &pb.OpenBufferByPath{
				ProjectId:  projectId,
				WorktreeId: 7,
				Path:       "main.rs",
			},
		},
	})

	forwarded := waitForPayload(t, host, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_OpenBufferByPath)
		return ok
	})
	require.NotNil(t, forwarded, "the host did not receive the open buffer request")

	// The host answers with the buffer id; it must be relayed to the guest.
	host.send(&pb.Envelope{
		Id:           200,
		RespondingTo: proto.Uint32(forwarded.GetId()),
		Payload: &pb.Envelope_OpenBufferResponse{
			OpenBufferResponse: &pb.OpenBufferResponse{BufferId: 99},
		},
	})

	relayed := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_OpenBufferResponse)
		return ok
	})
	require.NotNil(t, relayed, "the guest did not receive the open buffer response")
	assert.Equal(t, uint64(99), relayed.Payload.(*pb.Envelope_OpenBufferResponse).OpenBufferResponse.BufferId)

	// The host sends the buffer state to the guest, addressed by peer id.
	host.send(&pb.Envelope{
		Id: 201,
		Payload: &pb.Envelope_CreateBufferForPeer{
			CreateBufferForPeer: &pb.CreateBufferForPeer{
				ProjectId: projectId,
				PeerId:    guestPeer,
				Variant: &pb.CreateBufferForPeer_State{
					State: &pb.BufferState{Id: 99, BaseText: "fn main() {}"},
				},
			},
		},
	})

	state := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		_, ok := e.Payload.(*pb.Envelope_CreateBufferForPeer)
		return ok
	})
	require.NotNil(t, state, "the guest did not receive the buffer state")
	assert.Equal(t, "fn main() {}", state.Payload.(*pb.Envelope_CreateBufferForPeer).CreateBufferForPeer.GetState().GetBaseText())

	// The host edits the buffer; the guest must receive the operation.
	op := &pb.Operation{
		Variant: &pb.Operation_Edit_{
			Edit: &pb.Operation_Edit{
				ReplicaId:        1,
				LamportTimestamp: 1,
				Version:          []*pb.VectorClockEntry{{ReplicaId: 1, Timestamp: 1}},
				Ranges:           []*pb.Range{{Start: 3, End: 3}},
				NewText:          []string{"// hi\n"},
			},
		},
	}
	host.send(&pb.Envelope{
		Id: 202,
		Payload: &pb.Envelope_UpdateBuffer{
			UpdateBuffer: &pb.UpdateBuffer{
				ProjectId:  projectId,
				BufferId:   99,
				Operations: []*pb.Operation{op},
			},
		},
	})

	edit := waitForPayload(t, guest, func(e *pb.Envelope) bool {
		ub, ok := e.Payload.(*pb.Envelope_UpdateBuffer)
		return ok && len(ub.UpdateBuffer.GetOperations()) > 0
	})
	require.NotNil(t, edit, "the guest did not receive the buffer edit")
	assert.Equal(t, "// hi\n", edit.Payload.(*pb.Envelope_UpdateBuffer).UpdateBuffer.GetOperations()[0].GetEdit().GetNewText()[0])

	// The server keeps the operation history so that late joiners catch up.
	state2 := rpc.projectBuffers.Get(99)
	require.NotNil(t, state2)
	_, _, ops, version := state2.snapshot()
	require.Len(t, ops, 1)
	require.NotEmpty(t, version)
}
