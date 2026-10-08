package primaryproof

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/identity/internal/passwordwork"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
)

// VerifyCurrentPasswordWriter uses the exact acquired credential/contact once.
// The bounded pure worker retains no SQL or attempt. Cancellation releases the
// caller's root while its computation slot remains occupied until CPU exit.
func (v *Verifier) VerifyCurrentPasswordWriter(ctx context.Context, a *aw.Attempt, action wp.Action, actor wp.ActorCheck, supplied string) (evidence wp.Evidence, resultErr error) {
	terminal := false
	defer func() {
		if resultErr != nil && !terminal {
			outcome := aw.UnavailableRollback
			if errors.Is(resultErr, ErrUnauthenticated) {
				outcome = aw.DeniedRollback
			}
			if err := a.Finish(outcome); err != nil {
				resultErr = ErrUnavailable
			}
		}
	}()
	if v == nil || v.hasher == nil || ctx == nil || supplied == "" || len(supplied) > 512 {
		return wp.Evidence{}, ErrUnavailable
	}
	switch action {
	case wp.MFABegin, wp.MFAConfirm, wp.MFAChallenge, wp.PasswordChange:
	default:
		return wp.Evidence{}, ErrUnavailable
	}
	st, err := store.NewWriter(a)
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	scope := store.SessionScope{InstallationID: actor.Subject.Realm.Installation, ApplicationID: actor.Subject.Realm.Application, EnvironmentID: actor.Subject.Realm.Environment}
	encoded, err := st.FindCurrentPassword(ctx, scope, actor.Subject.Person, actor.Subject.Epoch)
	if errors.Is(err, store.ErrSessionUnavailable) {
		return wp.Evidence{}, ErrUnauthenticated
	}
	if err != nil {
		terminal = true
		return wp.Evidence{}, ErrUnavailable
	}
	credentials, err := a.PlannedRows(aw.Credentials)
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	contacts, err := a.PlannedRows(aw.Emails)
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	rows := append([]aw.Row{{Table: aw.Persons, ID: actor.Subject.Person, Access: aw.ExistingUpdate}}, credentials...)
	rows = append(rows, contacts...)
	tx, err := a.ParticipantTx(ctx, aw.C, rows)
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	check := wp.PasswordCheck{Subject: actor.Subject, VerifiedHash: encoded}
	// FindCurrentPassword owns password lookup and its exact held-row checks.
	// This complementary read captures the selected immutable contact/credential
	// IDs and verified timestamp needed by the closed evidence factory.
	err = tx.QueryRowContext(ctx, `SELECT c.id,e.id,e.comparison_key,e.verified_at FROM public.identity_credentials c JOIN public.identity_emails e ON e.person_id=c.person_id WHERE c.person_id=$1 AND c.method='email_password' AND c.revoked_at IS NULL AND c.verifier_hash=$2 AND e.installation_id=$3 AND e.application_id=$4 AND e.verified_at IS NOT NULL ORDER BY e.id LIMIT 1`, actor.Subject.Person, encoded, scope.InstallationID, scope.ApplicationID).Scan(&check.CredentialID, &check.Contact.ID, &check.Contact.ComparisonKey, &check.Contact.VerifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return wp.Evidence{}, ErrUnauthenticated
	}
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	if err = a.CheckRows(aw.C, []aw.Row{{Table: aw.Credentials, ID: check.CredentialID, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: check.Contact.ID, Access: aw.ExistingUpdate}}); err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	key := "mfa-current:" + actor.Subject.Person.String()
	if action == wp.PasswordChange {
		key = "change-current:" + actor.Subject.Person.String()
	}
	result, err := passwordwork.Verify(ctx, v.hasher, key, supplied, encoded, false)
	if err != nil && !errors.Is(err, password.ErrInvalid) {
		return wp.Evidence{}, ErrUnavailable
	}
	if err != nil || !result.Verified {
		return wp.Evidence{}, ErrUnauthenticated
	}
	if err = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&check.VerifiedAt); err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	check.ValidUntil = check.VerifiedAt.Add(15 * time.Minute)
	evidence, err = wp.Password(a, action, check)
	if err != nil {
		return wp.Evidence{}, ErrUnavailable
	}
	return evidence, nil
}
