package zed

import (
	"sync"
)

// contactsStore keeps the contacts and the pending contact requests that
// zedex serves to its clients.
//
// Zed tracks three lists per user:
//   - contacts: users that accepted a contact request,
//   - incoming_requests: users that asked to become a contact,
//   - outgoing_requests: users this user asked to become a contact with.
type contactsStore struct {
	mu       sync.Mutex
	contacts map[uint64]map[uint64]bool
	incoming map[uint64]map[uint64]bool
	outgoing map[uint64]map[uint64]bool
}

func newContactsStore() *contactsStore {
	return &contactsStore{
		contacts: map[uint64]map[uint64]bool{},
		incoming: map[uint64]map[uint64]bool{},
		outgoing: map[uint64]map[uint64]bool{},
	}
}

// requestContact records an outgoing contact request and returns the
// requester's id.
func (s *contactsStore) requestContact(requesterId uint64, responderId uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outgoing[requesterId] == nil {
		s.outgoing[requesterId] = map[uint64]bool{}
	}
	s.outgoing[requesterId][responderId] = true
	if s.incoming[responderId] == nil {
		s.incoming[responderId] = map[uint64]bool{}
	}
	s.incoming[responderId][requesterId] = true
}

// respondToContactRequest accepts or declines a contact request. It returns
// whether the request was accepted.
func (s *contactsStore) respondToContactRequest(responderId uint64, requesterId uint64, accept bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.incoming[responderId], requesterId)
	delete(s.outgoing[requesterId], responderId)
	if !accept {
		return false
	}
	if s.contacts[responderId] == nil {
		s.contacts[responderId] = map[uint64]bool{}
	}
	if s.contacts[requesterId] == nil {
		s.contacts[requesterId] = map[uint64]bool{}
	}
	s.contacts[responderId][requesterId] = true
	s.contacts[requesterId][responderId] = true
	return true
}

// removeContact removes a contact, or cancels a pending request. It returns
// whether an accepted contact was removed.
func (s *contactsStore) removeContact(userId uint64, otherId uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.outgoing[userId], otherId)
	delete(s.incoming[userId], otherId)
	delete(s.outgoing[otherId], userId)
	delete(s.incoming[otherId], userId)
	if s.contacts[userId][otherId] {
		delete(s.contacts[userId], otherId)
		delete(s.contacts[otherId], userId)
		return true
	}
	return false
}

func (s *contactsStore) listContacts(userId uint64) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []uint64{}
	for id := range s.contacts[userId] {
		out = append(out, id)
	}
	return out
}

func (s *contactsStore) listIncoming(userId uint64) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []uint64{}
	for id := range s.incoming[userId] {
		out = append(out, id)
	}
	return out
}

func (s *contactsStore) listOutgoing(userId uint64) []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []uint64{}
	for id := range s.outgoing[userId] {
		out = append(out, id)
	}
	return out
}
