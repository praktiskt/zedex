package zed

import (
	"zedex/zed/pb"

	log "github.com/sirupsen/logrus"
)

// This file implements the shared project (pairing) part of Zed's collab
// protocol.
//
// A shared project lives inside a call room. The client that shares the
// project is its host; guests join the project and then open buffers in it.
// Buffer synchronization is a CRDT: the host and the guests exchange
// operations that carry a version vector, and the server relays them.

// projectHostConnection returns the connection id of the client that hosts a
// project, or 0 when the project is unknown.
func (rpc *RpcHandler) projectHostConnection(projectId uint64) uint32 {
	hostId := rpc.projects.hostUserId(projectId)
	if hostId == 0 {
		return 0
	}
	return rpc.latestConnectionId(hostId)
}

func (rpc *RpcHandler) projectParticipants(projectId uint64) []uint64 {
	project := rpc.projects.get(projectId)
	if project == nil {
		return nil
	}
	return project.participants
}

// broadcastToProject sends an envelope to every participant of a project
// except the sender.
func (rpc *RpcHandler) broadcastToProject(projectId uint64, env *pb.Envelope, except int) {
	for _, uid := range rpc.projectParticipants(projectId) {
		if int(uid) == except {
			continue
		}
		for _, connId := range rpc.userConnections.Get(uid) {
			pd := NewProtoDispatcherForConnection(rpc, connId)
			if pd == nil {
				continue
			}
			if err := pd.SendProtobuf(env); err != nil {
				log.Errorf("broadcast to project %v failed for connection %v: %v", projectId, connId, err)
			}
		}
	}
}

func (rpc *RpcHandler) handleShareProject(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_ShareProject) error {
	sp := msg.ShareProject
	project := rpc.projects.create(uint64(pd.userId), sp.GetWorktrees(), sp.GetFeatures())
	project.participants = []uint64{uint64(pd.userId)}

	// The project belongs to the room it was shared in. The other participants
	// of that room learn about the project through the room state, which is
	// how Zed shows shared projects in the collaboration panel.
	room := rpc.rooms.get(sp.GetRoomId())
	if room == nil {
		room = rpc.rooms.create(0)
	}
	room.join(uint64(pd.userId), pd.peerId)
	rootNames := []string{}
	for _, wt := range sp.GetWorktrees() {
		rootNames = append(rootNames, wt.GetRootName())
	}
	room.shareProject(uint64(pd.userId), project.id, rootNames)
	rpc.broadcastRoomUpdated(room, pd.userId)

	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_ShareProjectResponse{
			ShareProjectResponse: &pb.ShareProjectResponse{ProjectId: project.id},
		},
	}
	return pd.SendProtobuf(&resp)
}

func (rpc *RpcHandler) handleJoinProject(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_JoinProject) error {
	jp := msg.JoinProject
	project := rpc.projects.get(jp.GetProjectId())
	if project == nil {
		resp := pb.Envelope{
			Id:           pd.NextId(),
			RespondingTo: &envelope.Id,
			Payload:      &pb.Envelope_Ack{Ack: &pb.Ack{}},
		}
		return pd.SendProtobuf(&resp)
	}

	project.addParticipant(uint64(pd.userId))

	collaborators := []*pb.Collaborator{}
	for _, uid := range project.participants {
		for _, connId := range rpc.userConnections.Get(uid) {
			conn := rpc.connections.Get(connId)
			if conn == nil {
				continue
			}
			collaborators = append(collaborators, &pb.Collaborator{
				PeerId:    conn.peerId,
				ReplicaId: connId,
				UserId:    uid,
			})
		}
	}

	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_JoinProjectResponse{
			JoinProjectResponse: &pb.JoinProjectResponse{
				ProjectId:     project.id,
				ReplicaId:     uint32(pd.userId),
				Worktrees:     project.worktrees,
				Collaborators: collaborators,
				Role:          pb.ChannelRole_Member,
				Features:      project.features,
			},
		},
	}
	return pd.SendProtobuf(&resp)
}

func (rpc *RpcHandler) handleUnshareProject(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_UnshareProject) error {
	up := msg.UnshareProject
	project := rpc.projects.get(up.GetProjectId())
	if project != nil {
		project.removeParticipant(uint64(pd.userId))
		for _, room := range rpc.rooms.all() {
			room.unshareProject(uint64(pd.userId), up.GetProjectId())
			rpc.broadcastRoomUpdated(room, pd.userId)
		}
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

func (rpc *RpcHandler) handleLeaveProject(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_LeaveProject) error {
	lp := msg.LeaveProject
	project := rpc.projects.get(lp.GetProjectId())
	if project != nil {
		project.removeParticipant(uint64(pd.userId))
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

func (rpc *RpcHandler) handleUpdateProject(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_UpdateProject) error {
	up := msg.UpdateProject
	project := rpc.projects.get(up.GetProjectId())
	if project != nil {
		project.mu.Lock()
		project.worktrees = up.GetWorktrees()
		project.mu.Unlock()
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleCreateBufferForPeer relays the host's buffer snapshot to the peer that
// is joining the project.
func (rpc *RpcHandler) handleCreateBufferForPeer(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_CreateBufferForPeer) error {
	cb := msg.CreateBufferForPeer
	peer := cb.GetPeerId()
	if peer == nil {
		return nil
	}
	connId := rpc.connectionsByPeer.Get(keyForPeerId(peer))
	if connId == 0 {
		log.Errorf("no connection for peer %v", peer)
		return nil
	}
	target := NewProtoDispatcherForConnection(rpc, connId)
	if target == nil {
		return nil
	}
	relay := pb.Envelope{
		Id:               pd.NextId(),
		OriginalSenderId: pd.peerId,
		Payload: &pb.Envelope_CreateBufferForPeer{
			CreateBufferForPeer: cb,
		},
	}
	return target.SendProtobuf(&relay)
}

// handleUpdateBuffer relays buffer operations to the other participants of a
// project and records them so that clients joining later can catch up.
func (rpc *RpcHandler) handleUpdateBuffer(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_UpdateBuffer) error {
	ub := msg.UpdateBuffer
	projectId := ub.GetProjectId()

	state := rpc.projectBuffers.Get(ub.GetBufferId())
	if state == nil {
		state = newProjectBufferState()
		rpc.projectBuffers.Set(ub.GetBufferId(), state)
	}
	state.addCollaborator(uint64(pd.userId), pd.peerId)
	state.recordOperations(ub.GetOperations())
	rpc.rememberBuffer(projectId, ub.GetBufferId())

	// Zed's collab server sends buffer operations to the guests directly and
	// forwards them to the host, which applies them to the real file.
	hostConnId := rpc.projectHostConnection(projectId)
	isHost := hostConnId != 0 && hostConnId == pd.connId

	relay := pb.Envelope{
		Id:               pd.NextId(),
		OriginalSenderId: pd.peerId,
		Payload: &pb.Envelope_UpdateBuffer{
			UpdateBuffer: ub,
		},
	}
	if isHost {
		// The host sent the edit: the guests need to catch up.
		rpc.broadcastBufferOperations(projectId, &relay, pd.userId)
	} else {
		// A guest sent the edit: the other guests catch up, and the host
		// applies the operations to the file.
		rpc.broadcastBufferOperationsToGuests(projectId, &relay, pd.userId, hostConnId)
		if forwarded, err := rpc.forwardToHost(pd, envelope, projectId); err != nil {
			return err
		} else if forwarded {
			// The host answers the request; the response is relayed back to
			// the guest by relayForwardedResponse.
			return nil
		}
	}

	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleOpenBufferByPath opens (or creates) a buffer inside a shared project
// and returns the buffer id the client should use.
func (rpc *RpcHandler) handleOpenBufferByPath(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_OpenBufferByPath) error {
	ob := msg.OpenBufferByPath
	if forwarded, err := rpc.forwardToHost(pd, envelope, ob.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	bufferId := rpc.nextBufferId(ob.GetProjectId(), ob.GetWorktreeId(), ob.GetPath())

	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_OpenBufferResponse{
			OpenBufferResponse: &pb.OpenBufferResponse{BufferId: bufferId},
		},
	}
	return pd.SendProtobuf(&resp)
}

func (rpc *RpcHandler) handleOpenBufferById(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_OpenBufferById) error {
	ob := msg.OpenBufferById
	if forwarded, err := rpc.forwardToHost(pd, envelope, ob.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_OpenBufferResponse{
			OpenBufferResponse: &pb.OpenBufferResponse{BufferId: ob.GetId()},
		},
	}
	return pd.SendProtobuf(&resp)
}

// nextBufferId returns a stable buffer id for a path inside a project.
func (rpc *RpcHandler) nextBufferId(projectId uint64, worktreeId uint64, path string) uint64 {
	key := uint64(len(rpc.projectBuffers.Map())) + 1
	return key
}
