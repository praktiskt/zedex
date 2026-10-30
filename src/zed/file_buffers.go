package zed

import (
	"zedex/zed/pb"

	log "github.com/sirupsen/logrus"
)

// This file implements the file buffer part of Zed's collab protocol.
//
// When a guest opens a file in a shared project, the request is forwarded to
// the client that hosts the project. The host opens the file locally and
// answers with the buffer id; it then sends the buffer's initial state to the
// guest with `CreateBufferForPeer`. From that point on, edits are exchanged
// as CRDT operations through `UpdateBuffer`.
//
// Zed routes these messages by *project id* (entity messages) and addresses
// individual peers with their peer id, so the server needs to know which
// connection belongs to which user and which project a buffer belongs to.

// bufferInfo tracks a buffer that lives in a shared project.
type bufferInfo struct {
	projectId uint64
	bufferId  uint64
}

// projectBuffersByProject maps a project id to the buffers it contains.
func (rpc *RpcHandler) buffersOfProject(projectId uint64) []uint64 {
	rpc.projectBufferIds.mu.Lock()
	defer rpc.projectBufferIds.mu.Unlock()
	ids := []uint64{}
	for id, info := range rpc.projectBufferIds.buffers {
		if info.projectId == projectId {
			ids = append(ids, id)
		}
	}
	return ids
}

func (rpc *RpcHandler) rememberBuffer(projectId uint64, bufferId uint64) {
	rpc.projectBufferIds.mu.Lock()
	defer rpc.projectBufferIds.mu.Unlock()
	rpc.projectBufferIds.buffers[bufferId] = bufferInfo{projectId: projectId, bufferId: bufferId}
}

// handleSaveBuffer forwards a save request to the project host.
func (rpc *RpcHandler) handleSaveBuffer(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_SaveBuffer) error {
	sb := msg.SaveBuffer
	if forwarded, err := rpc.forwardToHost(pd, envelope, sb.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleCloseBuffer forwards a close request to the project host.
func (rpc *RpcHandler) handleCloseBuffer(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_CloseBuffer) error {
	cb := msg.CloseBuffer
	if forwarded, err := rpc.forwardToHost(pd, envelope, cb.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleUpdateBufferFile forwards a file replacement to the project host.
func (rpc *RpcHandler) handleUpdateBufferFile(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_UpdateBufferFile) error {
	ubf := msg.UpdateBufferFile
	if forwarded, err := rpc.forwardToHost(pd, envelope, ubf.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleReloadBuffers forwards a reload request to the project host.
func (rpc *RpcHandler) handleReloadBuffers(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_ReloadBuffers) error {
	rb := msg.ReloadBuffers
	if forwarded, err := rpc.forwardToHost(pd, envelope, rb.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_ReloadBuffersResponse{
			ReloadBuffersResponse: &pb.ReloadBuffersResponse{},
		},
	}
	return pd.SendProtobuf(&resp)
}

// handleSynchronizeBuffers forwards a synchronization request to the project
// host, which answers with the current version of each buffer.
func (rpc *RpcHandler) handleSynchronizeBuffers(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_SynchronizeBuffers) error {
	sb := msg.SynchronizeBuffers
	if forwarded, err := rpc.forwardToHost(pd, envelope, sb.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	versions := []*pb.BufferVersion{}
	for _, bv := range sb.GetBuffers() {
		if state := rpc.projectBuffers.Get(bv.GetId()); state != nil {
			_, _, _, version := state.snapshot()
			versions = append(versions, &pb.BufferVersion{Id: bv.GetId(), Version: version})
		}
	}
	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_SynchronizeBuffersResponse{
			SynchronizeBuffersResponse: &pb.SynchronizeBuffersResponse{Buffers: versions},
		},
	}
	return pd.SendProtobuf(&resp)
}

// handleOpenNewBuffer forwards a request to create a new untitled buffer to
// the project host.
func (rpc *RpcHandler) handleOpenNewBuffer(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_OpenNewBuffer) error {
	ob := msg.OpenNewBuffer
	if forwarded, err := rpc.forwardToHost(pd, envelope, ob.GetProjectId()); err != nil {
		return err
	} else if forwarded {
		return nil
	}
	resp := pb.Envelope{
		Id:           pd.NextId(),
		RespondingTo: &envelope.Id,
		Payload: &pb.Envelope_OpenBufferResponse{
			OpenBufferResponse: &pb.OpenBufferResponse{BufferId: rpc.nextBufferId(ob.GetProjectId(), 0, "")},
		},
	}
	return pd.SendProtobuf(&resp)
}

// handleAddProjectCollaborator tells the other participants of a project
// that a new collaborator joined.
func (rpc *RpcHandler) handleAddProjectCollaborator(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_AddProjectCollaborator) error {
	apc := msg.AddProjectCollaborator
	projectId := apc.GetProjectId()
	collaborator := apc.GetCollaborator()

	update := pb.Envelope{
		Id: pd.NextId(),
		Payload: &pb.Envelope_AddProjectCollaborator{
			AddProjectCollaborator: apc,
		},
	}
	rpc.broadcastToProject(projectId, &update, pd.userId)
	_ = collaborator

	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// handleRemoveProjectCollaborator tells the other participants that a
// collaborator left.
func (rpc *RpcHandler) handleRemoveProjectCollaborator(pd *ProtoDispatcher, envelope *pb.Envelope, msg *pb.Envelope_RemoveProjectCollaborator) error {
	rmpc := msg.RemoveProjectCollaborator
	update := pb.Envelope{
		Id: pd.NextId(),
		Payload: &pb.Envelope_RemoveProjectCollaborator{
			RemoveProjectCollaborator: rmpc,
		},
	}
	rpc.broadcastToProject(rmpc.GetProjectId(), &update, pd.userId)

	resp := pb.Envelope{RespondingTo: &envelope.Id, Payload: &pb.Envelope_Ack{Ack: &pb.Ack{}}}
	return pd.SendProtobuf(&resp)
}

// broadcastBufferOperations sends buffer operations to every participant of a
// project, except the sender.
func (rpc *RpcHandler) broadcastBufferOperations(projectId uint64, env *pb.Envelope, except int) {
	rpc.broadcastToProject(projectId, env, except)
}

// broadcastBufferOperationsToGuests sends buffer operations to the guests of a
// project, skipping the sender and the host (the host receives the request
// separately so that it can apply the operations to the file).
func (rpc *RpcHandler) broadcastBufferOperationsToGuests(projectId uint64, env *pb.Envelope, except int, hostConnId uint32) {
	project := rpc.projects.get(projectId)
	if project == nil {
		return
	}
	for _, uid := range project.participants {
		if int(uid) == except {
			continue
		}
		for _, connId := range rpc.userConnections.Get(uid) {
			if connId == hostConnId {
				continue
			}
			pd := NewProtoDispatcherForConnection(rpc, connId)
			if pd == nil {
				continue
			}
			if err := pd.SendProtobuf(env); err != nil {
				log.Errorf("broadcast buffer operations failed for connection %v: %v", connId, err)
			}
		}
	}
}

// logProjectState is a small helper used while debugging project sync.
func logProjectState(projectId uint64, buffers int) {
	log.Debugf("project %v has %v buffers", projectId, buffers)
}
