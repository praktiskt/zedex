package zed

import (
	"zedex/zed/pb"

	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

// Zed's collab server does not execute project requests itself. When a guest
// opens a buffer in a shared project, the server forwards the request to the
// client that hosts the project; the host executes it (it has the files) and
// sends the response back, which the server then relays to the guest.
//
// This file implements that forwarding.

// forwardedRequest remembers where a forwarded request came from so that the
// response can be relayed back to the original requester.
type forwardedRequest struct {
	requesterId int
	// requestId is the id of the request as the original requester sent it.
	// The response must use it as `responding_to` so that the requester can
	// match it with its pending request.
	requestId uint32
}

// forwardToHost forwards a request to the client hosting a project and
// remembers how to relay the response back.
func (rpc *RpcHandler) forwardToHost(pd *ProtoDispatcher, envelope *pb.Envelope, projectId uint64) (bool, error) {
	project := rpc.projects.get(projectId)
	if project == nil {
		return false, nil
	}
	hostId := project.hostUserId()
	if hostId == 0 || int(hostId) == pd.userId {
		// The host executes the request itself.
		return false, nil
	}
	if len(rpc.userConnections.Get(hostId)) == 0 {
		return false, nil
	}

	forwardId := pd.NextId()
	rpc.pendingForwards.Set(forwardId, forwardedRequest{
		requesterId: pd.userId,
		requestId:   envelope.GetId(),
	})

	forwarded := &pb.Envelope{
		Id:               forwardId,
		OriginalSenderId: pd.peerId,
		Payload:          envelope.Payload,
	}
	if err := NewProtoDispatcher(rpc, int(hostId)).SendProtobuf(forwarded); err != nil {
		rpc.pendingForwards.Delete(forwardId)
		return false, err
	}
	return true, nil
}

// relayForwardedResponse relays a response that the host produced for a
// forwarded request back to the client that originally asked for it.
func (rpc *RpcHandler) relayForwardedResponse(pd *ProtoDispatcher, envelope *pb.Envelope) bool {
	respondingTo := envelope.GetRespondingTo()
	if respondingTo == 0 {
		return false
	}
	pending := rpc.pendingForwards.Get(respondingTo)
	if pending.requesterId == 0 {
		return false
	}
	rpc.pendingForwards.Delete(respondingTo)

	relayed := &pb.Envelope{
		Id:               pd.NextId(),
		RespondingTo:     proto.Uint32(pending.requestId),
		OriginalSenderId: envelope.OriginalSenderId,
		Payload:          envelope.Payload,
	}
	requesterPD := NewProtoDispatcher(rpc, pending.requesterId)
	if requesterPD == nil {
		return true
	}
	if err := requesterPD.SendProtobuf(relayed); err != nil {
		log.Errorf("failed to relay forwarded response: %v", err)
		return true
	}
	return true
}
