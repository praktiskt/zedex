package zed

import (
	"zedex/zed/pb"

	log "github.com/sirupsen/logrus"
)

// This file implements Zed's calls (rooms). A room is the container for
// participants and the projects they share with each other.

// handleCreateRoom creates a call and makes the caller its first participant.
func (rpc *RpcHandler) handleCreateRoom(pd *ProtoDispatcher, envelope *pb.Envelope) error {
	room := rpc.rooms.create(0)
	room.join(uint64(pd.userId), pd.peerId)

	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_CreateRoomResponse{
			CreateRoomResponse: &pb.CreateRoomResponse{Room: room.toProto()},
		},
	}
	return pd.SendProtobuf(&resp)
}

// handleJoinRoom joins an existing call.
func (rpc *RpcHandler) handleJoinRoom(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_JoinRoom) error {
	jr := msg.JoinRoom
	room := rpc.rooms.get(jr.GetId())
	if room == nil {
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload: &pb.Envelope_Error{
				Error: &pb.Error{Message: "room not found"},
			},
		}
		return pd.SendProtobuf(&resp)
	}
	room.join(uint64(pd.userId), pd.peerId)

	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_JoinRoomResponse{
			JoinRoomResponse: &pb.JoinRoomResponse{Room: room.toProto()},
		},
	}
	if err := pd.SendProtobuf(&resp); err != nil {
		return err
	}
	rpc.broadcastRoomUpdated(room, pd.userId)
	return nil
}

// handleLeaveRoom removes the caller from a call.
func (rpc *RpcHandler) handleLeaveRoom(pd *ProtoDispatcher, envelope *pb.Envelope) error {
	for _, room := range rpc.rooms.all() {
		room.leave(uint64(pd.userId))
		rpc.broadcastRoomUpdated(room, pd.userId)
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleUpdateParticipantLocation records where a participant is and tells the
// other participants about it.
func (rpc *RpcHandler) handleUpdateParticipantLocation(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_UpdateParticipantLocation) error {
	upl := msg.UpdateParticipantLocation
	room := rpc.rooms.get(upl.GetRoomId())
	if room != nil {
		room.setLocation(uint64(pd.userId), upl.GetLocation())
		rpc.broadcastRoomUpdated(room, pd.userId)
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// broadcastRoomUpdated tells every participant of a room about its state.
func (rpc *RpcHandler) broadcastRoomUpdated(room *room, except int) {
	update := pb.Envelope{
		Id: rpc.NextId(),
		Payload: &pb.Envelope_RoomUpdated{
			RoomUpdated: &pb.RoomUpdated{Room: room.toProto()},
		},
	}
	for _, uid := range room.participantUserIds() {
		if int(uid) == except {
			continue
		}
		for _, connId := range rpc.userConnections.Get(uid) {
			pd := NewProtoDispatcherForConnection(rpc, connId)
			if pd == nil {
				continue
			}
			if err := pd.SendProtobuf(&update); err != nil {
				log.Errorf("failed to send room update: %v", err)
			}
		}
	}
}

// handleFollow follows a participant so that their screen can be watched.
func (rpc *RpcHandler) handleFollow(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_Follow) error {
	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_FollowResponse{
			FollowResponse: &pb.FollowResponse{},
		},
	}
	return pd.SendProtobuf(&resp)
}

// handleUnfollow stops following a participant.
func (rpc *RpcHandler) handleUnfollow(pd *ProtoDispatcher, envelope *pb.Envelope) error {
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}
