// Package vault encrypts TOTP seed material for the MFA factor store.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/ajent-social/amos/identity/mfa"
	"github.com/google/uuid"
)

const (
	formatVersion = byte(1)
	nonceSize     = 12
	maxKeyID      = 32
	maxKeyCount   = 4
	maxSeedBytes  = 256
	maxBlobBytes  = 4096
)

var ErrUnavailable = errors.New("MFA seed vault unavailable")

type Vault struct {
	active string
	keys   map[string][32]byte
}

// NewKeyring copies a bounded set of AES-256 keys. The returned keyring cannot
// be changed by mutating the caller's map or byte slices.
func NewKeyring(activeKeyID string, keys map[string][]byte) (*Vault, error) {
	if len(keys) == 0 || len(keys) > maxKeyCount || !validKeyID(activeKeyID) {
		return nil, ErrUnavailable
	}
	result := &Vault{active: activeKeyID, keys: make(map[string][32]byte, len(keys))}
	for id, material := range keys {
		if !validKeyID(id) || len(material) != 32 {
			return nil, ErrUnavailable
		}
		var key [32]byte
		copy(key[:], material)
		result.keys[id] = key
	}
	if _, ok := result.keys[activeKeyID]; !ok {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (v *Vault) Seal(ctx context.Context, _ *sql.Tx, scope mfa.Scope, factorID uuid.UUID, state mfa.FactorState, expires time.Time, purpose string, plaintext []byte) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || v == nil || !validBinding(scope, factorID, state, expires, purpose) || !validSeed(plaintext) {
		return nil, ErrUnavailable
	}
	key, ok := v.keys[v.active]
	if !ok {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrUnavailable
	}
	id := []byte(v.active)
	out := make([]byte, 2+len(id)+nonceSize, 2+len(id)+nonceSize+len(plaintext)+aead.Overhead())
	out[0] = formatVersion
	out[1] = byte(len(id))
	copy(out[2:], id)
	copy(out[2+len(id):], nonce)
	out = aead.Seal(out, nonce, plaintext, associatedData(scope, factorID, state, expires, purpose, v.active))
	if len(out) > maxBlobBytes {
		return nil, ErrUnavailable
	}
	return out, nil
}

func (v *Vault) Open(ctx context.Context, _ *sql.Tx, scope mfa.Scope, factorID uuid.UUID, state mfa.FactorState, expires time.Time, purpose string, ciphertext []byte) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || v == nil || !validBinding(scope, factorID, state, expires, purpose) || len(ciphertext) < 2+1+nonceSize+16 || len(ciphertext) > maxBlobBytes || ciphertext[0] != formatVersion {
		return nil, ErrUnavailable
	}
	idLen := int(ciphertext[1])
	if idLen < 1 || idLen > maxKeyID || len(ciphertext) < 2+idLen+nonceSize+16 {
		return nil, ErrUnavailable
	}
	id := string(ciphertext[2 : 2+idLen])
	if !validKeyID(id) {
		return nil, ErrUnavailable
	}
	key, ok := v.keys[id]
	if !ok {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	nonceStart := 2 + idLen
	nonce := ciphertext[nonceStart : nonceStart+nonceSize]
	plaintext, err := aead.Open(nil, nonce, ciphertext[nonceStart+nonceSize:], associatedData(scope, factorID, state, expires, purpose, id))
	if err != nil || !validSeed(plaintext) {
		wipe(plaintext)
		return nil, ErrUnavailable
	}
	return plaintext, nil
}

func validBinding(scope mfa.Scope, factorID uuid.UUID, state mfa.FactorState, expires time.Time, purpose string) bool {
	if !validV7(scope.InstallationID) || !validV7(scope.ApplicationID) || !validV7(scope.EnvironmentID) || !validV7(scope.PersonID) || !validV7(factorID) || purpose != "totp.seed.v1" {
		return false
	}
	switch state {
	case mfa.FactorPending:
		return !expires.IsZero() && expires.Nanosecond()%1000 == 0
	case mfa.FactorActive:
		return expires.IsZero()
	default:
		return false
	}
}

func validSeed(seed []byte) bool {
	if len(seed) == 0 || len(seed) > maxSeedBytes {
		return false
	}
	// The only accepted material is the canonical unpadded Base32 secret
	// emitted by the RFC 6238 enrollment flow (20 random bytes).
	if len(seed) != 32 {
		return false
	}
	for _, c := range seed {
		if !isBase32Byte(c) {
			return false
		}
	}
	return true
}

func isBase32Byte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '2' && c <= '7':
		return true
	default:
		return false
	}
}

func validKeyID(id string) bool {
	if len(id) == 0 || len(id) > 32 || !isAlphaNumeric(id[0]) {
		return false
	}
	for _, c := range id {
		if !isAlphaNumeric(byte(c)) && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func isAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func validV7(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}

func associatedData(scope mfa.Scope, factorID uuid.UUID, state mfa.FactorState, expires time.Time, purpose, keyID string) []byte {
	data := make([]byte, 0, 32+16*5+8+1+2+len(purpose)+len(keyID))
	data = append(data, []byte("AMOS\x00identity\x00mfa-seed\x00v1")...)
	data = append(data, byte(len(keyID)))
	data = append(data, keyID...)
	data = append(data, byte(len(purpose)))
	data = append(data, purpose...)
	data = append(data, scope.InstallationID[:]...)
	data = append(data, scope.ApplicationID[:]...)
	data = append(data, scope.EnvironmentID[:]...)
	data = append(data, scope.PersonID[:]...)
	data = append(data, factorID[:]...)
	data = append(data, byte(len(state)))
	data = append(data, []byte(state)...)
	var expiry int64
	if !expires.IsZero() {
		expiry = expires.UTC().UnixMicro()
	}
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(expiry))
	data = append(data, encoded[:]...)
	return data
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
