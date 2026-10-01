package vault

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/mfa"
	"github.com/google/uuid"
)

func TestVaultT3_15AEADBindingAndRotation(t *testing.T) {
	ctx := context.Background()
	ids := makeIDs(t)
	expiry := time.Date(2026, 10, 1, 20, 0, 0, 123456000, time.UTC)
	seed := []byte("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP")
	oldKey := bytes.Repeat([]byte{0x31}, 32)
	newKey := bytes.Repeat([]byte{0x42}, 32)
	old, err := NewKeyring("local-v1", map[string][]byte{"local-v1": oldKey})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := old.Seal(ctx, nil, ids.scope, ids.factor, mfa.FactorPending, expiry, "totp.seed.v1", seed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, seed) {
		t.Fatal("ciphertext contains the plaintext seed")
	}
	rotated, err := NewKeyring("local-v2", map[string][]byte{"local-v1": oldKey, "local-v2": newKey})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := rotated.Open(ctx, nil, ids.scope, ids.factor, mfa.FactorPending, expiry, "totp.seed.v1", sealed)
	if err != nil || !bytes.Equal(opened, seed) {
		t.Fatal("rotation did not open old envelope")
	}
	wipe(opened)

	otherScope := ids.scope
	otherScope.PersonID = newID(t)
	otherFactor := newID(t)
	for name, args := range map[string]struct {
		scope   mfa.Scope
		factor  uuid.UUID
		state   mfa.FactorState
		expires time.Time
		purpose string
	}{
		"person":  {otherScope, ids.factor, mfa.FactorPending, expiry, "totp.seed.v1"},
		"factor":  {ids.scope, otherFactor, mfa.FactorPending, expiry, "totp.seed.v1"},
		"state":   {ids.scope, ids.factor, mfa.FactorActive, time.Time{}, "totp.seed.v1"},
		"expiry":  {ids.scope, ids.factor, mfa.FactorPending, expiry.Add(time.Microsecond), "totp.seed.v1"},
		"purpose": {ids.scope, ids.factor, mfa.FactorPending, expiry, "other-purpose"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := rotated.Open(ctx, nil, args.scope, args.factor, args.state, args.expires, args.purpose, sealed); err == nil {
				t.Fatal("opened ciphertext under a different binding")
			}
		})
	}
	corrupt := append([]byte(nil), sealed...)
	corrupt[len(corrupt)-1] ^= 0x80
	if _, err := rotated.Open(ctx, nil, ids.scope, ids.factor, mfa.FactorPending, expiry, "totp.seed.v1", corrupt); err == nil {
		t.Fatal("opened tampered ciphertext")
	}
}

func TestVaultT3_15RejectsBadKeyringAndExpiryPrecision(t *testing.T) {
	key := bytes.Repeat([]byte{0x53}, 32)
	for _, tc := range []struct {
		name   string
		active string
		keys   map[string][]byte
	}{
		{"missing-active", "local-v2", map[string][]byte{"local-v1": key}},
		{"bad-key-size", "local-v1", map[string][]byte{"local-v1": key[:16]}},
		{"too-many-keys", "local-v1", map[string][]byte{"local-v1": key, "v2": key, "v3": key, "v4": key, "v5": key}},
		{"delimiter-key-id", "local:v1", map[string][]byte{"local:v1": key}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewKeyring(tc.active, tc.keys); err == nil {
				t.Fatal("accepted invalid keyring")
			}
		})
	}
	v, err := NewKeyring("local-v1", map[string][]byte{"local-v1": key})
	if err != nil {
		t.Fatal(err)
	}
	ids := makeIDs(t)
	badExpiry := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond).Add(time.Nanosecond)
	if _, err := v.Seal(context.Background(), nil, ids.scope, ids.factor, mfa.FactorPending, badExpiry, "totp.seed.v1", []byte("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP")); err == nil {
		t.Fatal("accepted expiry which PostgreSQL cannot preserve for AAD")
	}
}

type idsForVault struct {
	scope  mfa.Scope
	factor uuid.UUID
}

func makeIDs(t *testing.T) idsForVault {
	t.Helper()
	return idsForVault{scope: mfa.Scope{InstallationID: newID(t), ApplicationID: newID(t), EnvironmentID: newID(t), PersonID: newID(t)}, factor: newID(t)}
}

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
