package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"zedex/utils"
)

// GuestProvider is the name of the provider anonymous sessions are signed in
// with.
const GuestProvider = "guest"

// Manager authenticates users and issues access tokens.
//
// The manager keeps no durable state. A user is identified by an id derived
// from the subject their provider reports, and the profile that travels with
// the access token.
type Manager struct {
	cfg *Config

	// providers are the OAuth providers users can sign in with, keyed by
	// name.
	providers map[string]Provider

	// order is the order the providers are shown in on the login page.
	order []string

	// primary is the provider whose logins are used as usernames as they
	// are.
	primary string

	// identityKey derives the user id from a provider identity.
	identityKey []byte

	// sessionKey signs the access tokens.
	sessionKey []byte

	// tokenTTL is how long an access token stays valid.
	tokenTTL time.Duration

	// profiles holds the profiles of the users that signed in on this
	// server. The access token carries the profile as well, so this is only
	// a cache that lets the server answer with a fresher avatar.
	profiles *utils.ConcurrentMap[uint64, Profile]
}

// New builds a manager from a configuration.
func New(cfg *Config) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("auth: no configuration")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	profiles := utils.NewConcurrentMap[uint64, Profile]()

	manager := &Manager{
		cfg:         cfg,
		providers:   map[string]Provider{},
		order:       []string{},
		primary:     cfg.PrimaryProvider(),
		identityKey: []byte(cfg.IdentitySecret),
		sessionKey:  []byte(cfg.SessionSecret),
		tokenTTL:    time.Duration(cfg.TokenTTLSeconds) * time.Second,
		profiles:    &profiles,
	}
	for _, providerCfg := range cfg.Providers {
		var provider Provider
		switch providerCfg.Type {
		case ProviderTypeGitHub:
			provider = NewGitHubProvider(providerCfg)
		case ProviderTypeOIDC:
			provider = NewOIDCProvider(providerCfg)
		default:
			return nil, fmt.Errorf("auth: unknown provider type %q", providerCfg.Type)
		}
		manager.providers[provider.Name()] = provider
		manager.order = append(manager.order, provider.Name())
	}
	return manager, nil
}

// Config returns the configuration the manager was built from.
func (m *Manager) Config() *Config { return m.cfg }

// TokenTTL is how long the access tokens stay valid.
func (m *Manager) TokenTTL() time.Duration { return m.tokenTTL }

// Providers returns the providers in the order they are shown on the login
// page.
func (m *Manager) Providers() []Provider {
	out := make([]Provider, 0, len(m.order))
	for _, name := range m.order {
		out = append(out, m.providers[name])
	}
	return out
}

// Provider returns the provider with the given name.
func (m *Manager) Provider(name string) (Provider, bool) {
	provider, ok := m.providers[name]
	return provider, ok
}

// Register adds a provider to the manager. A provider with the same name is
// replaced.
func (m *Manager) Register(provider Provider) {
	m.providers[provider.Name()] = provider
	for _, name := range m.order {
		if name == provider.Name() {
			return
		}
	}
	m.order = append(m.order, provider.Name())
}

// AllowGuest reports whether the anonymous sign-in is available.
func (m *Manager) AllowGuest() bool { return m.cfg.GuestsAllowed() }

// DeriveUserID turns a provider identity into the numeric id Zed uses for
// the user. The same identity always yields the same id.
func (m *Manager) DeriveUserID(provider string, subject string) uint64 {
	mac := hmac.New(sha256.New, m.identityKey)
	mac.Write([]byte(provider))
	mac.Write([]byte{0})
	mac.Write([]byte(subject))

	value := binary.BigEndian.Uint32(mac.Sum(nil)[:4]) & 0x7fffffff
	if value == 0 {
		value = 1
	}
	return uint64(value)
}

// UsernameFor returns the name a user goes by. The logins of the primary
// provider are used as they are, the logins of every other provider get the
// name of the provider appended so that usernames stay unique.
func (m *Manager) UsernameFor(provider string, login string) string {
	login = strings.TrimSpace(login)
	if login == "" {
		login = "user"
	}
	if provider == "" || provider == m.primary || provider == GuestProvider {
		return login
	}
	return login + "@" + provider
}

// Mint issues an access token for a profile. The user id is derived from the
// identity the provider reported.
func (m *Manager) Mint(profile *Profile) (uint64, string, error) {
	uid := m.DeriveUserID(profile.Provider, profile.Subject)
	token, err := m.MintFor(uid, profile)
	if err != nil {
		return 0, "", err
	}
	return uid, token, nil
}

// MintFor issues an access token for a profile of a known user.
func (m *Manager) MintFor(uid uint64, profile *Profile) (string, error) {
	now := time.Now()
	claims := &Claims{
		UID:       uid,
		Provider:  profile.Provider,
		Subject:   profile.Subject,
		Username:  m.UsernameFor(profile.Provider, profile.Username),
		Name:      profile.Name,
		Email:     profile.Email,
		AvatarURL: profile.AvatarURL,
		IssuedAt:  now,
	}
	if m.tokenTTL > 0 {
		claims.ExpiresAt = now.Add(m.tokenTTL)
	}

	token, err := m.mint(claims)
	if err != nil {
		return "", err
	}
	m.profiles.Set(uid, *profile)
	return token, nil
}

// MintGuest issues an access token for an anonymous session. The subject is
// what makes the session unique, the name is used as it is and does not get
// a provider suffix.
func (m *Manager) MintGuest(uid uint64, subject string, username string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UID:      uid,
		Provider: GuestProvider,
		Subject:  subject,
		Username: username,
		IssuedAt: now,
	}
	if m.tokenTTL > 0 {
		claims.ExpiresAt = now.Add(m.tokenTTL)
	}

	token, err := m.mint(claims)
	if err != nil {
		return "", err
	}
	m.profiles.Set(uid, Profile{
		Provider: GuestProvider,
		Subject:  claims.Subject,
		Username: username,
	})
	return token, nil
}

// RandomSubject returns a random identifier. Anonymous sessions use one so
// that every session gets its own identity.
func RandomSubject() (string, error) {
	return randomString(16)
}

// Verify checks an access token and returns the claims it makes.
func (m *Manager) Verify(token string) (*Claims, error) {
	claims, err := m.verify(token)
	if err != nil {
		return nil, err
	}
	if claims.Expired(time.Now()) {
		return nil, fmt.Errorf("%w: the token expired", ErrInvalidToken)
	}
	return claims, nil
}

// Remember stores the profile of a user so that the server can answer with
// it even when the access token does not carry it.
func (m *Manager) Remember(uid uint64, profile Profile) {
	m.profiles.Set(uid, profile)
}

// Profile returns the profile the server knows for a user.
func (m *Manager) Profile(uid uint64) (Profile, bool) {
	var profile Profile
	found := false
	m.profiles.Transaction(func(values map[uint64]Profile) map[uint64]Profile {
		profile, found = values[uid]
		return values
	})
	return profile, found
}

// Username returns the name the server knows a user by. It prefers the
// cached profile over the claims so that a user keeps the same name when the
// provider renamed them.
func (m *Manager) Username(claims *Claims) string {
	if claims == nil {
		return ""
	}
	if profile, ok := m.Profile(claims.UID); ok && profile.Username != "" {
		return m.UsernameFor(profile.Provider, profile.Username)
	}
	return claims.Username
}

// AvatarURL returns the avatar the server knows for a user.
func (m *Manager) AvatarURL(claims *Claims) string {
	if claims == nil {
		return ""
	}
	if profile, ok := m.Profile(claims.UID); ok && profile.AvatarURL != "" {
		return profile.AvatarURL
	}
	return claims.AvatarURL
}

// Name returns the display name the server knows for a user.
func (m *Manager) Name(claims *Claims) string {
	if claims == nil {
		return ""
	}
	if profile, ok := m.Profile(claims.UID); ok && profile.Name != "" {
		return profile.Name
	}
	return claims.Name
}

// GithubLogin returns the GitHub account the server knows for a user.
func (m *Manager) GithubLogin(claims *Claims) string {
	if claims == nil {
		return ""
	}
	if profile, ok := m.Profile(claims.UID); ok && profile.GithubLogin != "" {
		return profile.GithubLogin
	}
	return claims.Username
}
