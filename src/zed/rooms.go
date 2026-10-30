package zed

import (
	"sync"

	"zedex/zed/pb"
)

// Rooms are Zed's calls. A call holds participants, and participants can share
// projects with each other. Sharing a project means: the host asks the server
// to share it (ShareProject), the server assigns a project id and tells the
// other participants about it, and the participants then open the project
// (JoinProject) and exchange buffer operations.
//
// Without rooms, a shared project is invisible to the other participants: the
// client only learns about shared projects from the room it is in.

// roomParticipant is a participant of a room.
type roomParticipant struct {
	userId   uint64
	peerId   *pb.PeerId
	projects []*pb.ParticipantProject
	location *pb.ParticipantLocation
	role     pb.ChannelRole
	index    uint32
}

// room is a call.
type room struct {
	mu sync.Mutex

	id uint64
	// channelId is set when the room belongs to a channel.
	channelId uint64
	// participants maps a user id to its participation.
	participants map[uint64]*roomParticipant
	// nextIndex hands out the participant indexes used for colours.
	nextIndex uint32
}

func newRoom(id uint64, channelId uint64) *room {
	return &room{
		id:           id,
		channelId:    channelId,
		participants: map[uint64]*roomParticipant{},
	}
}

// join adds a participant to the room.
func (r *room) join(userId uint64, peerId *pb.PeerId) *roomParticipant {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.participants[userId]; ok {
		p.peerId = peerId
		return p
	}
	index := r.nextIndex
	r.nextIndex++
	p := &roomParticipant{
		userId: userId,
		peerId: peerId,
		index:  index,
		role:   pb.ChannelRole_Guest,
	}
	r.participants[userId] = p
	return p
}

func (r *room) leave(userId uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.participants, userId)
}

// setLocation records where a participant currently is.
func (r *room) setLocation(userId uint64, location *pb.ParticipantLocation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.participants[userId]; ok {
		p.location = location
	}
}

// shareProject adds a project to a participant's list.
func (r *room) shareProject(userId uint64, projectId uint64, worktreeRootNames []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.participants[userId]
	if !ok {
		return
	}
	for _, project := range p.projects {
		if project.Id == projectId {
			return
		}
	}
	p.projects = append(p.projects, &pb.ParticipantProject{
		Id:                projectId,
		WorktreeRootNames: worktreeRootNames,
	})
}

// unshareProject removes a project from a participant's list.
func (r *room) unshareProject(userId uint64, projectId uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.participants[userId]
	if !ok {
		return
	}
	filtered := []*pb.ParticipantProject{}
	for _, project := range p.projects {
		if project.Id != projectId {
			filtered = append(filtered, project)
		}
	}
	p.projects = filtered
}

// toProto converts the room into its protobuf representation.
func (r *room) toProto() *pb.Room {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := &pb.Room{
		Id:           r.id,
		Participants: []*pb.Participant{},
	}
	for _, p := range r.participants {
		out.Participants = append(out.Participants, &pb.Participant{
			UserId:           p.userId,
			PeerId:           p.peerId,
			Projects:         p.projects,
			Location:         p.location,
			ParticipantIndex: p.index,
			Role:             p.role,
		})
	}
	return out
}

// participantUserIds returns the ids of the users in the room.
func (r *room) participantUserIds() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []uint64{}
	for id := range r.participants {
		out = append(out, id)
	}
	return out
}

// roomsStore keeps the rooms of the server.
type roomsStore struct {
	mu sync.Mutex
	// rooms maps a room id to the room.
	rooms map[uint64]*room
	// nextId hands out room ids.
	nextId uint64
}

func newRoomsStore() *roomsStore {
	return &roomsStore{rooms: map[uint64]*room{}, nextId: 1}
}

func (s *roomsStore) create(channelId uint64) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextId
	s.nextId++
	r := newRoom(id, channelId)
	s.rooms[id] = r
	return r
}

func (s *roomsStore) get(id uint64) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms[id]
}

// channelRoom returns the room of a channel, creating it when needed.
func (s *roomsStore) channelRoom(channelId uint64) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rooms {
		if r.channelId == channelId {
			return r
		}
	}
	id := s.nextId
	s.nextId++
	r := newRoom(id, channelId)
	s.rooms[id] = r
	return r
}

// all returns every room of the server.
func (s *roomsStore) all() []*room {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*room, 0, len(s.rooms))
	for _, r := range s.rooms {
		out = append(out, r)
	}
	return out
}

func (s *roomsStore) remove(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, id)
}
