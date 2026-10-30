package zed

import (
	"sync"
	"time"

	"zedex/zed/pb"
)

// notificationsStore keeps the notifications that zedex serves to its
// clients. Notifications are broadcast to every connected client so that all
// sessions see the same notifications.
type notificationsStore struct {
	mu            sync.Mutex
	nextId        uint64
	notifications []*pb.Notification
}

func newNotificationsStore() *notificationsStore {
	return &notificationsStore{nextId: 1}
}

func (s *notificationsStore) add(n *pb.Notification) *pb.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n == nil {
		return nil
	}
	if n.Id == 0 {
		n.Id = s.nextId
	}
	if n.Id >= s.nextId {
		s.nextId = n.Id + 1
	}
	if n.Timestamp == 0 {
		n.Timestamp = uint64(time.Now().Unix())
	}
	s.notifications = append(s.notifications, n)
	return n
}

func (s *notificationsStore) list(beforeId uint64) []*pb.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*pb.Notification{}
	for _, n := range s.notifications {
		if beforeId != 0 && n.Id >= beforeId {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (s *notificationsStore) update(n *pb.Notification) *pb.Notification {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.notifications {
		if existing.Id == n.GetId() {
			s.notifications[i] = n
			return n
		}
	}
	return nil
}

func (s *notificationsStore) remove(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.notifications {
		if existing.Id == id {
			s.notifications = append(s.notifications[:i], s.notifications[i+1:]...)
			return true
		}
	}
	return false
}

// contactsStore keeps the contacts (users that have accepted each other's
// contact requests) known to the server.
type contactsStore struct {
	mu       sync.Mutex
	contacts map[uint64]map[uint64]bool
}

func newContactsStore() *contactsStore {
	return &contactsStore{contacts: map[uint64]map[uint64]bool{}}
}

func (s *contactsStore) add(userId uint64, contactId uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.contacts[userId] == nil {
		s.contacts[userId] = map[uint64]bool{}
	}
	s.contacts[userId][contactId] = true
}

func (s *contactsStore) remove(userId uint64, contactId uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.contacts[userId], contactId)
}

func (s *contactsStore) list(userId uint64) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []uint64{}
	for id := range s.contacts[userId] {
		out = append(out, id)
	}
	return out
}

// sharedProject is a project that has been shared into a call room.
type sharedProject struct {
	mu           sync.Mutex
	id           uint64
	ownerId      uint64
	worktrees    []*pb.WorktreeMetadata
	features     []string
	participants []uint64
}

// hostUserId returns the client that owns (hosts) the project.
func (p *sharedProject) hostUserId() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ownerId
}

func (p *sharedProject) addParticipant(userId uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range p.participants {
		if id == userId {
			return
		}
	}
	p.participants = append(p.participants, userId)
}

func (p *sharedProject) removeParticipant(userId uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	filtered := []uint64{}
	for _, id := range p.participants {
		if id != userId {
			filtered = append(filtered, id)
		}
	}
	p.participants = filtered
}

type projectsStore struct {
	mu       sync.Mutex
	nextId   uint64
	projects map[uint64]*sharedProject
}

func newProjectsStore() *projectsStore {
	return &projectsStore{nextId: 1, projects: map[uint64]*sharedProject{}}
}

func (s *projectsStore) create(ownerId uint64, worktrees []*pb.WorktreeMetadata, features []string) *sharedProject {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := &sharedProject{
		id:        s.nextId,
		ownerId:   ownerId,
		worktrees: worktrees,
		features:  features,
	}
	s.nextId++
	s.projects[p.id] = p
	return p
}

// hostUserId returns the user that owns (hosts) the project.
func (s *projectsStore) hostUserId(id uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.projects[id]
	if p == nil {
		return 0
	}
	return p.ownerId
}

func (s *projectsStore) get(id uint64) *sharedProject {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projects[id]
}

func (s *projectsStore) remove(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.projects, id)
}

// projectBufferIndex keeps track of which project a buffer belongs to.
type projectBufferIndex struct {
	mu      sync.Mutex
	buffers map[uint64]bufferInfo
}

func newProjectBufferIndex() *projectBufferIndex {
	return &projectBufferIndex{buffers: map[uint64]bufferInfo{}}
}
