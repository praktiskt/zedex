package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// pendingTTL is how long a sign in may take before it is thrown away.
const pendingTTL = 10 * time.Minute

// Pending is a sign in that is in progress.
//
// Zed starts a sign in by opening the login page in a browser and waits for
// a callback on a local port. The page has to remember where to send the
// browser back to, so the details of the sign in are kept here until the
// provider sends the user back.
type Pending struct {
	// ID is the random identifier of the sign in. It is used as the state
	// of the OAuth request, which ties the callback to this sign in.
	ID string

	// Port is the local port Zed waits for the callback on.
	Port string

	// PublicKey is the key the access token is encrypted with.
	PublicKey string

	// RedirectURI is the URI the provider sends the user back to.
	RedirectURI string

	// Provider is the name of the provider the user is signing in with.
	Provider string

	// CodeVerifier is the PKCE verifier of the OAuth request.
	CodeVerifier string

	// CreatedAt is when the sign in started.
	CreatedAt time.Time
}

// Flow keeps track of the sign ins that are in progress.
type Flow struct {
	mu      sync.Mutex
	pending map[string]*Pending
}

// NewFlow builds a flow that keeps no sign ins.
func NewFlow() *Flow {
	return &Flow{pending: map[string]*Pending{}}
}

// Create starts a sign in and returns it.
func (f *Flow) Create(port string, publicKey string, redirectURI string) (*Pending, error) {
	id, err := randomString(32)
	if err != nil {
		return nil, err
	}

	pending := &Pending{
		ID:          id,
		Port:        port,
		PublicKey:   publicKey,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweepLocked()
	f.pending[id] = pending
	return pending, nil
}

// Get returns the sign in with the given id.
func (f *Flow) Get(id string) *Pending {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending[id]
}

// Start records which provider a sign in is going to use.
func (f *Flow) Start(id string, provider string, codeVerifier string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	pending, ok := f.pending[id]
	if !ok {
		return fmt.Errorf("unknown sign in")
	}
	if pending.Provider != "" {
		return fmt.Errorf("the sign in already started with %s", pending.Provider)
	}
	pending.Provider = provider
	pending.CodeVerifier = codeVerifier
	return nil
}

// Take returns the sign in with the given id and forgets about it. A sign in
// can only be completed once.
func (f *Flow) Take(id string) *Pending {
	f.mu.Lock()
	defer f.mu.Unlock()

	pending, ok := f.pending[id]
	if !ok {
		return nil
	}
	delete(f.pending, id)
	return pending
}

// sweepLocked throws away the sign ins that took too long.
func (f *Flow) sweepLocked() {
	cutoff := time.Now().Add(-pendingTTL)
	for id, pending := range f.pending {
		if pending.CreatedAt.Before(cutoff) {
			delete(f.pending, id)
		}
	}
}

// NewCodeVerifier returns a PKCE verifier and the challenge that goes with
// it.
func NewCodeVerifier() (string, string, error) {
	verifier, err := randomString(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// randomString returns a random URL safe string.
func randomString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
