package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T, ttl int64) *Manager {
	t.Helper()

	manager, err := New(&Config{
		IdentitySecret:  "identity-secret",
		SessionSecret:   "session-secret",
		TokenTTLSeconds: ttl,
		AllowGuest:      boolPtr(true),
		Providers: []ProviderConfig{
			{
				Name:         "github",
				Type:         ProviderTypeGitHub,
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				Primary:      true,
			},
			{
				Name:         "okta",
				Type:         ProviderTypeOIDC,
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				Issuer:       "https://issuer.example.com",
			},
		},
	})
	if err != nil {
		t.Fatalf("could not build the manager: %v", err)
	}
	return manager
}

func boolPtr(value bool) *bool { return &value }

func TestTokenRoundTrip(t *testing.T) {
	manager := testManager(t, 0)

	profile := &Profile{
		Provider:    "github",
		Subject:     "12345",
		Username:    "magnus",
		Name:        "Magnus Smith",
		Email:       "magnus@example.com",
		AvatarURL:   "https://avatars.githubusercontent.com/u/12345?v=4",
		GithubLogin: "magnus",
	}

	uid, token, err := manager.Mint(profile)
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}
	if uid == 0 {
		t.Fatalf("the user id should not be zero")
	}

	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("could not verify the token: %v", err)
	}
	if claims.UID != uid {
		t.Errorf("the token carries user id %d, expected %d", claims.UID, uid)
	}
	if claims.Provider != "github" {
		t.Errorf("the token carries provider %q, expected %q", claims.Provider, "github")
	}
	if claims.Subject != "12345" {
		t.Errorf("the token carries subject %q, expected %q", claims.Subject, "12345")
	}
	if claims.Username != "magnus" {
		t.Errorf("the token carries username %q, expected %q", claims.Username, "magnus")
	}
	if !claims.ExpiresAt.IsZero() {
		t.Errorf("a token without a lifetime should not expire")
	}
}

func TestTokenFitsTheRsaPlaintext(t *testing.T) {
	manager := testManager(t, 0)

	// A profile with every field at its limit has to fit into the
	// plaintext an RSA-2048 key can carry, because Zed encrypts the access
	// token with the key it generates when it signs in.
	profile := &Profile{
		Provider:  strings.Repeat("p", 16),
		Subject:   strings.Repeat("s", 48),
		Username:  strings.Repeat("u", 48),
		Name:      strings.Repeat("n", 48),
		Email:     strings.Repeat("e", 48),
		AvatarURL: "https://avatars.githubusercontent.com/u/" + strings.Repeat("a", 40) + "?v=4",
	}

	uid, token, err := manager.Mint(profile)
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}

	claims, err := manager.verify(token)
	if err != nil {
		t.Fatalf("could not verify the token: %v", err)
	}
	if claims.UID != uid {
		t.Errorf("the token carries user id %d, expected %d", claims.UID, uid)
	}
	if claims.Username != profile.Username {
		t.Errorf("the token carries username %q, expected %q", claims.Username, profile.Username)
	}

	// The signature is part of the plaintext as well.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token should have three parts, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("could not decode the payload: %v", err)
	}
	plaintext := len(payload) + tokenSignatureBytes
	if plaintext > maxTokenPlaintext {
		t.Errorf("the token encrypts %d bytes, it has to fit into %d", plaintext, maxTokenPlaintext)
	}
}

func TestTokenRejectsTampering(t *testing.T) {
	manager := testManager(t, 0)

	_, token, err := manager.Mint(&Profile{Provider: "github", Subject: "1", Username: "magnus"})
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token should have three parts, got %d", len(parts))
	}

	// Flip a character of the payload.
	payload := []byte(parts[1])
	if payload[0] == 'A' {
		payload[0] = 'B'
	} else {
		payload[0] = 'A'
	}
	tampered := parts[0] + "." + string(payload) + "." + parts[2]
	if _, err := manager.Verify(tampered); err == nil {
		t.Errorf("a token with a changed payload should be rejected")
	}

	// Change the signature.
	signature := []byte(parts[2])
	if signature[0] == 'A' {
		signature[0] = 'B'
	} else {
		signature[0] = 'A'
	}
	tampered = parts[0] + "." + parts[1] + "." + string(signature)
	if _, err := manager.Verify(tampered); err == nil {
		t.Errorf("a token with a changed signature should be rejected")
	}

	// A token of another server.
	other, err := New(&Config{
		IdentitySecret: "identity-secret",
		SessionSecret:  "another-secret",
		Providers: []ProviderConfig{
			{Name: "github", Type: ProviderTypeGitHub, ClientID: "id", ClientSecret: "secret"},
		},
	})
	if err != nil {
		t.Fatalf("could not build the other manager: %v", err)
	}
	if _, err := other.Verify(token); err == nil {
		t.Errorf("a token of another server should be rejected")
	}
}

func TestTokenExpires(t *testing.T) {
	manager := testManager(t, 0)

	expired := &Claims{
		UID:       1,
		Provider:  "github",
		Subject:   "1",
		Username:  "magnus",
		IssuedAt:  time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	token, err := manager.mint(expired)
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}
	if _, err := manager.Verify(token); err == nil {
		t.Errorf("an expired token should be rejected")
	}
}

func TestUserIDIsDerived(t *testing.T) {
	manager := testManager(t, 0)

	first := manager.DeriveUserID("github", "12345")
	second := manager.DeriveUserID("github", "12345")
	if first != second {
		t.Errorf("the same identity should always yield the same user id")
	}
	if first == 0 {
		t.Errorf("the user id should not be zero")
	}
	if first > 0x7fffffff {
		t.Errorf("the user id %d does not fit into the signed 32 bit integer Zed uses", first)
	}

	if manager.DeriveUserID("github", "12345") == manager.DeriveUserID("github", "12346") {
		t.Errorf("different identities should yield different user ids")
	}
	if manager.DeriveUserID("github", "12345") == manager.DeriveUserID("okta", "12345") {
		t.Errorf("the same subject at another provider should yield another user id")
	}

	// Another server derives other user ids, so that two servers do not
	// hand out the same identities.
	other, err := New(&Config{
		IdentitySecret: "another-identity-secret",
		SessionSecret:  "session-secret",
		Providers: []ProviderConfig{
			{Name: "github", Type: ProviderTypeGitHub, ClientID: "id", ClientSecret: "secret"},
		},
	})
	if err != nil {
		t.Fatalf("could not build the other manager: %v", err)
	}
	if other.DeriveUserID("github", "12345") == first {
		t.Errorf("another server should derive another user id")
	}
}

func TestUsernameGetsTheProviderSuffix(t *testing.T) {
	manager := testManager(t, 0)

	if name := manager.UsernameFor("github", "magnus"); name != "magnus" {
		t.Errorf("the login of the primary provider should be used as it is, got %q", name)
	}
	if name := manager.UsernameFor("okta", "magnus"); name != "magnus@okta" {
		t.Errorf("the login of another provider should get the provider appended, got %q", name)
	}
	if name := manager.UsernameFor("guest", "brisk-acorn"); name != "brisk-acorn" {
		t.Errorf("an anonymous session should keep its name, got %q", name)
	}
}

func TestGuestsAreAllowedByDefault(t *testing.T) {
	manager, err := New(&Config{
		IdentitySecret: "identity-secret",
		SessionSecret:  "session-secret",
		Providers: []ProviderConfig{
			{Name: "github", Type: ProviderTypeGitHub, ClientID: "id", ClientSecret: "secret"},
		},
	})
	if err != nil {
		t.Fatalf("could not build the manager: %v", err)
	}
	if !manager.AllowGuest() {
		t.Errorf("guests should be allowed unless the configuration says otherwise")
	}

	denied := false
	manager, err = New(&Config{
		IdentitySecret: "identity-secret",
		SessionSecret:  "session-secret",
		AllowGuest:     &denied,
		Providers: []ProviderConfig{
			{Name: "github", Type: ProviderTypeGitHub, ClientID: "id", ClientSecret: "secret"},
		},
	})
	if err != nil {
		t.Fatalf("could not build the manager: %v", err)
	}
	if manager.AllowGuest() {
		t.Errorf("guests should not be allowed when the configuration says so")
	}
}

func TestMintGuest(t *testing.T) {
	manager := testManager(t, 0)

	subject, err := RandomSubject()
	if err != nil {
		t.Fatalf("could not build a subject: %v", err)
	}
	uid := manager.DeriveUserID(GuestProvider, subject)

	token, err := manager.MintGuest(uid, subject, "brisk-acorn")
	if err != nil {
		t.Fatalf("could not mint a guest token: %v", err)
	}
	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("could not verify the guest token: %v", err)
	}
	if claims.UID != uid {
		t.Errorf("the guest token carries user id %d, expected %d", claims.UID, uid)
	}
	if claims.Username != "brisk-acorn" {
		t.Errorf("the guest token carries username %q, expected %q", claims.Username, "brisk-acorn")
	}
	if claims.Provider != GuestProvider {
		t.Errorf("the guest token carries provider %q, expected %q", claims.Provider, GuestProvider)
	}
}

func TestProfileIsRemembered(t *testing.T) {
	manager := testManager(t, 0)

	profile := &Profile{Provider: "github", Subject: "1", Username: "magnus", AvatarURL: "https://example.com/a.png"}
	uid, _, err := manager.Mint(profile)
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}

	stored, ok := manager.Profile(uid)
	if !ok {
		t.Fatalf("the profile should be remembered")
	}
	if stored.AvatarURL != profile.AvatarURL {
		t.Errorf("the remembered avatar is %q, expected %q", stored.AvatarURL, profile.AvatarURL)
	}
}

func TestTokenLifetime(t *testing.T) {
	manager := testManager(t, 3600)

	_, token, err := manager.Mint(&Profile{Provider: "github", Subject: "1", Username: "magnus"})
	if err != nil {
		t.Fatalf("could not mint a token: %v", err)
	}
	claims, err := manager.Verify(token)
	if err != nil {
		t.Fatalf("could not verify the token: %v", err)
	}
	if claims.ExpiresAt.IsZero() {
		t.Fatalf("a token with a lifetime should expire")
	}
	if claims.ExpiresAt.Before(time.Now().Add(time.Minute)) {
		t.Errorf("the token expires too soon: %v", claims.ExpiresAt)
	}
}
