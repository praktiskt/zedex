package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// tokenVersion is the version byte of the token format.
	tokenVersion = 1

	// tokenPrefix marks a token as one of ours.
	tokenPrefix = "v1"

	// maxTokenPlaintext is the largest plaintext an RSA-2048 key can carry
	// with OAEP padding and a SHA-256 digest: 256 - 2*32 - 2.
	//
	// Zed generates a 2048 bit key pair when it signs in and expects the
	// server to encrypt the access token with it, so the token has to fit
	// in 190 bytes.
	maxTokenPlaintext = 190

	// tokenSignatureBytes is the length of the truncated HMAC that signs a
	// token. Truncating to 128 bits leaves a forgery at 2^128 attempts.
	tokenSignatureBytes = 16
)

// The fields of a token, in the order they are written.
const (
	fieldProvider = iota
	fieldSubject
	fieldUsername
	fieldName
	fieldAvatar
	fieldEmail
	fieldCount
)

// The maximum length of every field of a token. The limits keep the token
// inside the plaintext an RSA key can carry.
var fieldLimits = [fieldCount]int{
	fieldProvider: 16,
	fieldSubject:  48,
	fieldUsername: 48,
	fieldName:     48,
	fieldAvatar:   96,
	fieldEmail:    48,
}

// The fields that are dropped, least important first, when the token does
// not fit into the plaintext budget.
var dropOrder = []int{fieldEmail, fieldAvatar, fieldName, fieldSubject}

// ErrInvalidToken is returned for a token that zedex cannot trust.
var ErrInvalidToken = errors.New("invalid access token")

// Claims are the statements a token makes about a user.
type Claims struct {
	// UID is the numeric user id.
	UID uint64

	// Provider is the name of the provider the identity comes from.
	Provider string

	// Subject is the identifier the provider knows the user by.
	Subject string

	// Username is the login the user goes by.
	Username string

	// Name is the display name of the user.
	Name string

	// Email is the address of the user.
	Email string

	// AvatarURL is the picture of the user.
	AvatarURL string

	// IssuedAt is when the token was minted.
	IssuedAt time.Time

	// ExpiresAt is when the token stops being valid. It is the zero time
	// for a token that never expires.
	ExpiresAt time.Time
}

// Expired reports whether the token is no longer valid.
func (c *Claims) Expired(now time.Time) bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return now.After(c.ExpiresAt)
}

// encode serializes the claims into the payload of a token.
func encodeClaims(claims *Claims, budget int) ([]byte, error) {
	values := [fieldCount][]byte{
		fieldProvider: []byte(claims.Provider),
		fieldSubject:  []byte(claims.Subject),
		fieldUsername: []byte(claims.Username),
		fieldName:     []byte(claims.Name),
		fieldAvatar:   []byte(claims.AvatarURL),
		fieldEmail:    []byte(claims.Email),
	}
	for i := range values {
		if len(values[i]) > fieldLimits[i] {
			values[i] = values[i][:fieldLimits[i]]
		}
	}

	dropped := map[int]bool{}
	remaining := dropOrder
	for {
		payload, err := buildPayload(claims, values, dropped)
		if err != nil {
			return nil, err
		}
		if len(payload) <= budget {
			return payload, nil
		}
		if len(remaining) == 0 {
			return nil, fmt.Errorf("the access token does not fit into %d bytes", budget)
		}
		dropped[remaining[0]] = true
		remaining = remaining[1:]
	}
}

// buildPayload writes the claims into the binary payload of a token.
func buildPayload(claims *Claims, values [fieldCount][]byte, dropped map[int]bool) ([]byte, error) {
	buf := make([]byte, 0, 64)
	buf = append(buf, tokenVersion)

	// The first bit says whether the token expires, the bits after it
	// say which fields are present.
	flags := byte(0)
	if !claims.ExpiresAt.IsZero() {
		flags |= 1
	}
	for i := 0; i < fieldCount; i++ {
		if !dropped[i] {
			flags |= 1 << (i + 1)
		}
	}
	buf = append(buf, flags)

	uid := make([]byte, 4)
	binary.BigEndian.PutUint32(uid, uint32(claims.UID))
	buf = append(buf, uid...)

	issued := make([]byte, 4)
	binary.BigEndian.PutUint32(issued, uint32(claims.IssuedAt.Unix()))
	buf = append(buf, issued...)

	expires := make([]byte, 4)
	if !claims.ExpiresAt.IsZero() {
		binary.BigEndian.PutUint32(expires, uint32(claims.ExpiresAt.Unix()))
	}
	buf = append(buf, expires...)

	for i := 0; i < fieldCount; i++ {
		if dropped[i] {
			continue
		}
		length := make([]byte, 2)
		binary.BigEndian.PutUint16(length, uint16(len(values[i])))
		buf = append(buf, length...)
		buf = append(buf, values[i]...)
	}
	return buf, nil
}

// decodeClaims parses the payload of a token.
func decodeClaims(payload []byte) (*Claims, error) {
	if len(payload) < 14 {
		return nil, fmt.Errorf("%w: the payload is too short", ErrInvalidToken)
	}
	if payload[0] != tokenVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidToken, payload[0])
	}
	flags := payload[1]
	if flags&^byte(1|1<<1|1<<2|1<<3|1<<4|1<<5|1<<6) != 0 {
		return nil, fmt.Errorf("%w: unknown flags %d", ErrInvalidToken, flags)
	}

	claims := &Claims{
		UID:      uint64(binary.BigEndian.Uint32(payload[2:6])),
		IssuedAt: time.Unix(int64(binary.BigEndian.Uint32(payload[6:10])), 0),
	}
	if flags&1 != 0 {
		claims.ExpiresAt = time.Unix(int64(binary.BigEndian.Uint32(payload[10:14])), 0)
	}

	rest := payload[14:]
	fields := make([]string, fieldCount)
	for i := 0; i < fieldCount; i++ {
		if flags&(1<<(i+1)) == 0 {
			continue
		}
		if len(rest) < 2 {
			return nil, fmt.Errorf("%w: the payload ends in the middle of a field", ErrInvalidToken)
		}
		length := int(binary.BigEndian.Uint16(rest[:2]))
		rest = rest[2:]
		if len(rest) < length {
			return nil, fmt.Errorf("%w: the payload ends in the middle of a field", ErrInvalidToken)
		}
		fields[i] = string(rest[:length])
		rest = rest[length:]
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: the payload has trailing bytes", ErrInvalidToken)
	}

	claims.Provider = fields[fieldProvider]
	claims.Subject = fields[fieldSubject]
	claims.Username = fields[fieldUsername]
	claims.Name = fields[fieldName]
	claims.AvatarURL = fields[fieldAvatar]
	claims.Email = fields[fieldEmail]
	return claims, nil
}

// sign returns the payload together with the signature that covers it.
func (m *Manager) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, m.sessionKey)
	mac.Write(payload)
	return mac.Sum(nil)[:tokenSignatureBytes]
}

// mint turns the claims into an access token.
func (m *Manager) mint(claims *Claims) (string, error) {
	payload, err := encodeClaims(claims, maxTokenPlaintext-tokenSignatureBytes)
	if err != nil {
		return "", err
	}
	signature := m.sign(payload)

	body := make([]byte, 0, len(payload)+len(signature))
	body = append(body, payload...)
	body = append(body, signature...)

	encoder := base64.RawURLEncoding
	return tokenPrefix + "." + encoder.EncodeToString(payload) + "." + encoder.EncodeToString(signature), nil
}

// verify checks the signature of a token and returns its claims.
func (m *Manager) verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return nil, fmt.Errorf("%w: malformed token", ErrInvalidToken)
	}

	encoder := base64.RawURLEncoding
	payload, err := encoder.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed payload", ErrInvalidToken)
	}
	signature, err := encoder.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed signature", ErrInvalidToken)
	}
	if len(signature) != tokenSignatureBytes {
		return nil, fmt.Errorf("%w: malformed signature", ErrInvalidToken)
	}
	if !hmac.Equal(signature, m.sign(payload)) {
		return nil, fmt.Errorf("%w: bad signature", ErrInvalidToken)
	}
	return decodeClaims(payload)
}
