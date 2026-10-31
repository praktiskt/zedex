package auth

import (
	"testing"
)

func TestFlowKeepsOneSignInAtATime(t *testing.T) {
	flow := NewFlow()

	pending, err := flow.Create("1234", "public-key", "https://zedex.example.com/oauth/callback")
	if err != nil {
		t.Fatalf("could not create a sign in: %v", err)
	}
	if pending.ID == "" {
		t.Fatalf("the sign in should have an id")
	}
	if pending.Port != "1234" {
		t.Errorf("the sign in should remember the port, got %q", pending.Port)
	}
	if pending.RedirectURI != "https://zedex.example.com/oauth/callback" {
		t.Errorf("the sign in should remember the redirect uri, got %q", pending.RedirectURI)
	}

	if flow.Get(pending.ID) == nil {
		t.Errorf("the sign in should be found")
	}

	verifier, challenge, err := NewCodeVerifier()
	if err != nil {
		t.Fatalf("could not build a code verifier: %v", err)
	}
	if verifier == "" || challenge == "" {
		t.Fatalf("the code verifier and its challenge should not be empty")
	}
	if verifier == challenge {
		t.Errorf("the code verifier and its challenge should differ")
	}

	if err := flow.Start(pending.ID, "github", verifier); err != nil {
		t.Fatalf("could not start the sign in: %v", err)
	}

	// A sign in cannot be started twice.
	if err := flow.Start(pending.ID, "okta", verifier); err == nil {
		t.Errorf("a sign in should only be started once")
	}

	started := flow.Get(pending.ID)
	if started.Provider != "github" {
		t.Errorf("the sign in should use the github provider, got %q", started.Provider)
	}
	if started.CodeVerifier != verifier {
		t.Errorf("the sign in should remember the code verifier")
	}

	// Taking a sign in removes it, a sign in can only be completed once.
	taken := flow.Take(pending.ID)
	if taken == nil {
		t.Fatalf("the sign in should be found")
	}
	if flow.Get(pending.ID) != nil {
		t.Errorf("a sign in should be gone once it was taken")
	}
	if flow.Take(pending.ID) != nil {
		t.Errorf("a sign in should only be taken once")
	}
}

func TestFlowThrowsAwayOldSignIns(t *testing.T) {
	flow := NewFlow()

	pending, err := flow.Create("1234", "public-key", "https://zedex.example.com/oauth/callback")
	if err != nil {
		t.Fatalf("could not create a sign in: %v", err)
	}

	flow.mu.Lock()
	flow.pending[pending.ID].CreatedAt = flow.pending[pending.ID].CreatedAt.Add(-2 * pendingTTL)
	flow.mu.Unlock()

	other, err := flow.Create("1234", "public-key", "https://zedex.example.com/oauth/callback")
	if err != nil {
		t.Fatalf("could not create a sign in: %v", err)
	}
	if flow.Get(pending.ID) != nil {
		t.Errorf("an old sign in should be thrown away")
	}
	if flow.Get(other.ID) == nil {
		t.Errorf("the new sign in should be kept")
	}
}
