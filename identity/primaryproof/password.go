// Package primaryproof verifies the current primary password for factor
// enrollment and step-up. It requires durable MFA transport admission.
package primaryproof

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"time"
)

var ErrUnauthenticated = errors.New("current primary credential unavailable")
var ErrUnavailable = errors.New("primary verification unavailable")

type Verifier struct{ hasher *password.Hasher }

func New(hasher *password.Hasher) (*Verifier, error) {
	if hasher == nil {
		return nil, ErrUnavailable
	}
	return &Verifier{hasher: hasher}, nil
}

// VerifyCurrentPassword holds the authoritative credential lock until the
// caller commits factor changes and session rotation. The password budget
// accepts this exact person-bound key only once per admitted MFA mutation.
func (v *Verifier) VerifyCurrentPassword(ctx context.Context, tx *sql.Tx, principal identity.Principal, supplied string) (authproof.VerifiedCredential, error) {
	empty := authproof.VerifiedCredential{}
	if aw.SelectLegacy() != nil {
		return empty, ErrUnavailable
	}
	if v == nil || v.hasher == nil || ctx == nil || tx == nil || principal.PersonID().Version() != 7 {
		return empty, ErrUnavailable
	}
	st, err := store.New(tx)
	if err != nil {
		return empty, ErrUnavailable
	}
	encoded, err := st.FindCurrentPassword(ctx, store.SessionScope{InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID(), EnvironmentID: principal.EnvironmentID()}, principal.PersonID(), principal.SecurityEpoch())
	if errors.Is(err, store.ErrSessionUnavailable) {
		return empty, ErrUnauthenticated
	}
	if err != nil {
		return empty, ErrUnavailable
	}
	result, err := v.hasher.Verify(ctx, "mfa-current:"+principal.PersonID().String(), supplied, encoded, nil)
	if err != nil && !errors.Is(err, password.ErrInvalid) {
		return empty, ErrUnavailable
	}
	if err != nil || !result.Verified {
		return empty, ErrUnauthenticated
	}
	now := time.Now().UTC()
	return authproof.NewVerifiedCredential(principal.PersonID(), principal.InstallationID(), principal.ApplicationID(), principal.EnvironmentID(), principal.SecurityEpoch(), "email_password", now, "aal1", now.Add(12*time.Hour))
}
