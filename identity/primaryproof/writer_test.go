package primaryproof

import (
	"context"
	"errors"
	"testing"
	"time"

	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

func TestNativeWriterPureRejectsUnrootedPrimary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	verifier, err := New(&password.Hasher{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []wp.Action{wp.MFABegin, wp.MFAConfirm, wp.MFAChallenge, wp.PasswordChange, wp.PasswordSignIn, 0} {
		evidence, err := verifier.VerifyCurrentPasswordWriter(ctx, &aw.Attempt{}, action, wp.ActorCheck{}, "synthetic password")
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal("unrooted primary not unavailable")
		}
		if _, err = evidence.PasswordSnapshot(&aw.Attempt{}, action); err == nil {
			t.Fatal("unrooted primary disclosed snapshot")
		}
	}
}
