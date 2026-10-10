package passwordwork

import (
	"context"
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/password/policy"
	"golang.org/x/crypto/argon2"
)

type budgetKey struct{}

// This double checks context preservation, not durable transport admission.
// The required-service test separately exercises the native protection guard.
type testBudget struct{}

func (testBudget) Allow(ctx context.Context, key string) error {
	if ctx.Value(budgetKey{}) != key {
		return password.ErrUnavailable
	}
	return nil
}

func testHasher(t *testing.T) *password.Hasher {
	t.Helper()
	blocklist, err := policy.New(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	h, err := password.New(blocklist, testBudget{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), budgetKey{}, "test"), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func awaitSlots(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(slots) != count {
		if time.Now().After(deadline) {
			t.Fatalf("computation slots = %d, want %d", len(slots), count)
		}
		runtime.Gosched()
	}
}

func TestRealHasherAndOriginalContext(t *testing.T) {
	h := testHasher(t)
	ctx := bounded(t)
	phrase := "a sufficiently long synthetic password"
	encoded, err := Hash(ctx, h, "test", phrase)
	if err != nil || !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("hash unavailable: %v", err)
	}
	for _, tc := range []struct {
		name, phrase, encoded string
		want                  bool
	}{
		{"current", phrase, encoded, true},
		{"wrong", "a different synthetic password", encoded, false},
		{"unknown", phrase, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Verify(ctx, h, "test", tc.phrase, tc.encoded, true)
			if err != nil || got.Verified != tc.want || got.Replacement != "" {
				t.Fatalf("unexpected verification result or error: verified=%t replacement=%t err=%v", got.Verified, got.Replacement != "", err)
			}
		})
	}
	if got, err := Verify(ctx, h, "different-budget", phrase, encoded, false); !errors.Is(err, password.ErrBusy) || got != (Verification{}) {
		t.Fatal("missing matching context budget did not fail closed")
	}
	if got, err := Hash(ctx, h, "test", "short"); !errors.Is(err, password.ErrInvalid) || got != "" {
		t.Fatal("existing password policy bypassed")
	}
	awaitSlots(t, 0)
}

func TestAdmissionBeforeComputation(t *testing.T) {
	valid := bounded(t)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		h    *password.Hasher
		key  string
	}{
		{"nil-context", nil, new(password.Hasher), "test"},
		{"no-deadline", context.Background(), new(password.Hasher), "test"},
		{"expired", expired, new(password.Hasher), "test"},
		{"nil-hasher", valid, nil, "test"},
		{"empty-key", valid, new(password.Hasher), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := run(tc.ctx, tc.h, tc.key, func() (string, error) {
				t.Error("invalid admission started computation")
				return "forbidden", nil
			})
			if err == nil || value != "" {
				t.Fatal("invalid admission accepted")
			}
		})
	}
	awaitSlots(t, 0)
}

func TestLegacyRehashIsOnlyCaptured(t *testing.T) {
	h := testHasher(t)
	ctx := bounded(t)
	phrase := "legacy synthetic password for bounded work"
	salt := []byte("test-only-salt16")
	if len(salt) != 16 {
		t.Fatal("invalid synthetic salt")
	}
	hash := argon2.IDKey([]byte(phrase), salt, 2, 32768, 1, 32)
	encoded := "$argon2id$v=19$m=32768,t=2,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
	without, err := Verify(ctx, h, "test", phrase, encoded, false)
	if err != nil || !without.Verified || without.Replacement != "" {
		t.Fatal("disabled rehash did not preserve verification")
	}
	with, err := Verify(ctx, h, "test", phrase, encoded, true)
	if err != nil || !with.Verified || !strings.HasPrefix(with.Replacement, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatal("legacy verification did not capture current encoding")
	}
	restored, err := Verify(ctx, h, "test", phrase, with.Replacement, false)
	if err != nil || !restored.Verified || restored.Replacement != "" {
		t.Fatal("captured encoding does not verify")
	}
	awaitSlots(t, 0)
}

func TestCanceledComputationsKeepSlotsUntilExit(t *testing.T) {
	awaitSlots(t, 0)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	returned := make(chan error, 2)
	ctx, cancel := context.WithCancel(bounded(t))
	for range 2 {
		go func() {
			value, err := run(ctx, new(password.Hasher), "test", func() (string, error) {
				started <- struct{}{}
				<-release
				return "late result", nil
			})
			if value != "" {
				returned <- errors.New("late computation escaped cancellation")
				return
			}
			returned <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("computation did not start")
		}
	}
	cancel()
	for range 2 {
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("waiter did not return cancellation: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("waiter waited for computation completion")
		}
	}
	if len(slots) != 2 {
		t.Error("canceled computations released admission before actual exit")
	}
	// Both public entry points share this bound and must not invoke the hasher.
	for _, op := range []func() error{
		func() error { _, err := Hash(bounded(t), new(password.Hasher), "test", "phrase"); return err },
		func() error {
			_, err := Verify(bounded(t), new(password.Hasher), "test", "phrase", "", false)
			return err
		},
	} {
		if err := op(); !errors.Is(err, password.ErrBusy) {
			t.Errorf("third computation was not rejected busy: %v", err)
		}
	}
	close(release)
	awaitSlots(t, 0) // An unbuffered late send cannot release its slot.
}

func TestCompletionCannotOverrideCancellationOrError(t *testing.T) {
	ctx, cancel := context.WithCancel(bounded(t))
	cancel()
	if value, err := accept(ctx, result[string]{value: "late"}); value != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("ready result overrode observable cancellation")
	}
	if value, err := accept(bounded(t), result[string]{value: "partial", err: password.ErrInvalid}); value != "" || !errors.Is(err, password.ErrInvalid) {
		t.Fatal("error exposed partial computation output")
	}
	if value, err := run(bounded(t), new(password.Hasher), "test", func() (string, error) { return "partial", password.ErrInvalid }); value != "" || !errors.Is(err, password.ErrInvalid) {
		t.Fatal("worker error exposed partial output")
	}
	awaitSlots(t, 0)
}
