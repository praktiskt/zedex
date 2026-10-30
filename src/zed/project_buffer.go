package zed

import (
	"sync"

	"zedex/zed/pb"
)

// projectBufferState is the shared state of a file buffer inside a shared
// project. Like channel notes, file buffers are CRDTs: clients own a replica
// id and send operations carrying a version vector. The server keeps the
// operation history so that clients joining later can rebuild the buffer and
// relays operations to the other collaborators of the project.
type projectBufferState struct {
	mu            sync.Mutex
	baseText      string
	epoch         uint64
	operations    []*pb.Operation
	version       []*pb.VectorClockEntry
	collaborators map[uint64]*pb.Collaborator
	nextReplicaId uint32
}

func newProjectBufferState() *projectBufferState {
	return &projectBufferState{
		baseText:      "",
		epoch:         1,
		operations:    []*pb.Operation{},
		version:       []*pb.VectorClockEntry{},
		collaborators: map[uint64]*pb.Collaborator{},
		nextReplicaId: FIRST_COLLAB_ID,
	}
}

func (s *projectBufferState) addCollaborator(userId uint64, peerId *pb.PeerId) *pb.Collaborator {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.collaborators[userId]; ok {
		return existing
	}
	replicaId := s.nextReplicaId
	s.nextReplicaId++
	collaborator := &pb.Collaborator{
		PeerId:    peerId,
		ReplicaId: replicaId,
		UserId:    userId,
	}
	s.collaborators[userId] = collaborator
	return collaborator
}

func (s *projectBufferState) removeCollaborator(userId uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collaborators, userId)
}

func (s *projectBufferState) collaboratorList() []*pb.Collaborator {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*pb.Collaborator, 0, len(s.collaborators))
	for _, c := range s.collaborators {
		out = append(out, c)
	}
	return out
}

func (s *projectBufferState) collaboratorUserIds() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint64, 0, len(s.collaborators))
	for id := range s.collaborators {
		out = append(out, id)
	}
	return out
}

func (s *projectBufferState) recordOperations(ops []*pb.Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		if op == nil {
			continue
		}
		s.operations = append(s.operations, op)
		switch v := op.Variant.(type) {
		case *pb.Operation_Edit_:
			edit := v.Edit
			if edit == nil {
				continue
			}
			s.observeVersion(edit.ReplicaId, edit.LamportTimestamp)
			for _, entry := range edit.Version {
				s.observeVersion(entry.ReplicaId, entry.Timestamp)
			}
		case *pb.Operation_Undo_:
			undo := v.Undo
			if undo == nil {
				continue
			}
			s.observeVersion(undo.ReplicaId, undo.LamportTimestamp)
			for _, entry := range undo.Version {
				s.observeVersion(entry.ReplicaId, entry.Timestamp)
			}
		}
	}
}

func (s *projectBufferState) observeVersion(replicaId uint32, timestamp uint32) {
	for _, entry := range s.version {
		if entry.ReplicaId == replicaId {
			if entry.Timestamp < timestamp {
				entry.Timestamp = timestamp
			}
			return
		}
	}
	s.version = append(s.version, &pb.VectorClockEntry{ReplicaId: replicaId, Timestamp: timestamp})
}

func (s *projectBufferState) snapshot() (string, uint64, []*pb.Operation, []*pb.VectorClockEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := make([]*pb.Operation, len(s.operations))
	copy(ops, s.operations)
	version := make([]*pb.VectorClockEntry, len(s.version))
	copy(version, s.version)
	return s.baseText, s.epoch, ops, version
}
