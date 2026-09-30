package password

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type localList struct{ ready bool }

func (l localList) Ready() bool                    { return l.ready }
func (localList) ContainsNormalized(s string) bool { return s == "this is a blocked password" }

type allowedBudget struct{}

func (allowedBudget) Allow(context.Context, string) error { return nil }
func hasher(t *testing.T) *Hasher {
	t.Helper()
	h, e := New(localList{true}, allowedBudget{}, 1)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func TestT3_4_CorrectWrongUnknownAndNormalization(t *testing.T) {
	h := hasher(t)
	ctx := context.Background()
	secret := "a lengthy cafe\u0301 password"
	encoded, e := h.Hash(ctx, "account", secret)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(encoded, "password") {
		t.Fatal("plaintext stored")
	}
	r, e := h.Verify(ctx, "account", "a lengthy café password", encoded, nil)
	if e != nil || !r.Verified {
		t.Fatalf("normalized verify: %+v %v", r, e)
	}
	for _, v := range []struct{ secret, hash string }{{"a different long password", encoded}, {secret, ""}} {
		r, e = h.Verify(ctx, "account", v.secret, v.hash, nil)
		if e != nil || r.Verified {
			t.Fatalf("unexpected verification %+v %v", r, e)
		}
	}
}
func TestT3_4_CostEncodingAndAdmissionBounds(t *testing.T) {
	h := hasher(t)
	ctx := context.Background()
	for _, encoded := range []string{strings.Repeat("x", 257), "$argon2id$v=19$m=4294967295,t=99999,p=1$c2FsdA$aGFzaA", "$argon2id$v=16$m=65536,t=3,p=1$c2FsdA$aGFzaA"} {
		if _, e := h.Verify(ctx, "account", "a suitably long password", encoded, nil); !errors.Is(e, ErrInvalid) {
			t.Fatalf("adversarial encoding accepted: %v", e)
		}
	}
	for _, s := range []string{"short", strings.Repeat("x", 513), string([]byte{0xff}), "this is a blocked password"} {
		if _, e := h.Hash(ctx, "account", s); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid enrollment accepted: %v", e)
		}
	}
	release, e := h.acquire(ctx, "account")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := h.Hash(ctx, "account", "a suitably long password"); !errors.Is(e, ErrBusy) {
		t.Fatalf("capacity not bounded: %v", e)
	}
	release()
	if _, e := New(localList{false}, allowedBudget{}, 1); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing policy accepted")
	}
}
func TestT3_4_LegacyUpgradePreservesProofOnPersistenceFailure(t *testing.T) {
	h := hasher(t)
	ctx := context.Background()
	secret := "a suitably long password"
	old, e := derive(secret, 32768, 2)
	if e != nil {
		t.Fatal(e)
	}
	called := false
	r, e := h.Verify(ctx, "account", secret, old, func(_ context.Context, previous, next string) error {
		called = true
		if previous != old {
			t.Error("not CAS-bound")
		}
		p, err := parse(next)
		if err != nil || p.memory != 65536 || p.iterations != 3 {
			t.Error("not current profile")
		}
		return errors.New("database unavailable")
	})
	if e != nil || !called || !r.Verified || !r.NeedsRehash || r.RehashPersisted {
		t.Fatalf("failed upgrade destroyed proof: %+v %v", r, e)
	}
}
