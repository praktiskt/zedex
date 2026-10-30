package zed

import (
	"testing"

	"zedex/zed/pb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContactRequestFlow walks through the flow of adding a friend:
//
//  1. A asks B to become a contact,
//  2. B sees an incoming request,
//  3. A sees an outgoing request,
//  4. B accepts,
//  5. both see each other as a contact and the pending requests are gone.
func TestContactRequestFlow(t *testing.T) {
	rpc := NewRpcHandler()
	server := startTestRPCServer(t, &rpc)

	a := dialTestClient(t, server, 7001)
	b := dialTestClient(t, server, 7002)

	hello := &pb.Envelope{Payload: &pb.Envelope_Hello{Hello: &pb.Hello{PeerId: &pb.PeerId{Id: 1}}}}
	a.send(hello)
	b.send(hello)

	// A asks B to become a contact.
	a.send(&pb.Envelope{
		Id: 1,
		Payload: &pb.Envelope_RequestContact{
			RequestContact: &pb.RequestContact{ResponderId: 7002},
		},
	})

	// B receives the incoming request.
	incoming := waitForPayload(t, b, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateContacts)
		return ok && len(uc.UpdateContacts.GetIncomingRequests()) > 0
	})
	require.NotNil(t, incoming, "B did not receive the incoming contact request")
	assert.Equal(t, uint64(7001), incoming.Payload.(*pb.Envelope_UpdateContacts).UpdateContacts.GetIncomingRequests()[0].GetRequesterId())

	// A sees the request as outgoing.
	outgoing := waitForPayload(t, a, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateContacts)
		return ok && len(uc.UpdateContacts.GetOutgoingRequests()) > 0
	})
	require.NotNil(t, outgoing, "A did not receive the outgoing contact request")
	assert.Equal(t, uint64(7002), outgoing.Payload.(*pb.Envelope_UpdateContacts).UpdateContacts.GetOutgoingRequests()[0])

	// B accepts.
	b.send(&pb.Envelope{
		Id: 2,
		Payload: &pb.Envelope_RespondToContactRequest{
			RespondToContactRequest: &pb.RespondToContactRequest{
				RequesterId: 7001,
				Response:    pb.ContactRequestResponse_Accept,
			},
		},
	})

	// B sees A as a contact and the incoming request is cleared.
	bUpdate := waitForPayload(t, b, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateContacts)
		return ok && len(uc.UpdateContacts.GetContacts()) > 0
	})
	require.NotNil(t, bUpdate, "B did not receive the contact")
	bContacts := bUpdate.Payload.(*pb.Envelope_UpdateContacts).UpdateContacts
	assert.Equal(t, uint64(7001), bContacts.GetContacts()[0].GetUserId())
	assert.Contains(t, bContacts.GetRemoveIncomingRequests(), uint64(7001))

	// A sees B as a contact and the outgoing request is cleared.
	aUpdate := waitForPayload(t, a, func(e *pb.Envelope) bool {
		uc, ok := e.Payload.(*pb.Envelope_UpdateContacts)
		return ok && len(uc.UpdateContacts.GetContacts()) > 0
	})
	require.NotNil(t, aUpdate, "A did not receive the contact")
	aContacts := aUpdate.Payload.(*pb.Envelope_UpdateContacts).UpdateContacts
	assert.Equal(t, uint64(7002), aContacts.GetContacts()[0].GetUserId())
	assert.Contains(t, aContacts.GetRemoveOutgoingRequests(), uint64(7002))
}
