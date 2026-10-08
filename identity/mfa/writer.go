package mfa

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ajent-social/amos/identity"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// NewWithWriter retains the exact native verifier/session and root. The existing
// SeedVault interface avoids the vault child-package import cycle; composition
// supplies its concrete native implementation and finite native policy.
// Run/Read, not a startup SQL probe, enforce active-root admission.
func NewWithWriter(root *aw.Root, cfg TxConfig) (*Service, error) {
	primary, ok := cfg.Primary.(*primaryproof.Verifier)
	sessions, sessionOK := cfg.Sessions.(*session.Service)
	if root == nil || !ok || primary == nil || !sessionOK || sessions == nil || cfg.Vault == nil || cfg.Policy == nil || cfg.Now != nil || cfg.Issuer == "" || len(cfg.Issuer) > 64 || strings.TrimSpace(cfg.Issuer) != cfg.Issuer {
		return nil, ErrConfiguration
	}
	return &Service{root: root, cfg: cfg, writerPrimary: primary, writerSessions: sessions}, nil
}
func mfaFinish(a *aw.Attempt, outcome aw.Outcome) aw.Outcome {
	if err := a.Finish(outcome); err != nil {
		return aw.UnavailableRollback
	}
	return outcome
}
func mfaProofFailure(a *aw.Attempt, err error) aw.Outcome {
	if errors.Is(err, wp.ErrDenied) {
		return mfaFinish(a, aw.DeniedRollback)
	}
	return mfaFinish(a, aw.UnavailableRollback)
}
func mfaParticipantFailure(err error) aw.Outcome {
	if errors.Is(err, ErrFactorAbsent) || errors.Is(err, ErrFactorExists) || errors.Is(err, primaryproof.ErrUnauthenticated) || errors.Is(err, session.ErrUnauthenticated) {
		return aw.DeniedRollback
	}
	return aw.UnavailableRollback
}
func mfaRows(rows []aw.Row) []aw.Row {
	out := make([]aw.Row, 0, len(rows))
	seen := make(map[aw.Row]bool)
	for _, r := range rows {
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	return out
}
func mfaAcquire(ctx context.Context, a *aw.Attempt) error {
	for _, p := range []aw.Phase{aw.P, aw.C, aw.H, aw.S, aw.W} {
		if err := a.Acquire(ctx, p); err != nil {
			return err
		}
	}
	return nil
}
func mfaRealm(scope Scope) aw.Realm {
	return aw.Realm{Installation: scope.InstallationID, Application: scope.ApplicationID, Environment: scope.EnvironmentID}
}
func primaryRows(ctx context.Context, tx *sql.Tx, scope Scope) ([]aw.Row, error) {
	var credential, email uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT c.id,e.id FROM public.identity_credentials c JOIN public.identity_emails e ON e.person_id=c.person_id WHERE c.person_id=$1 AND c.method='email_password' AND c.revoked_at IS NULL AND e.installation_id=$2 AND e.application_id=$3 AND e.verified_at IS NOT NULL ORDER BY e.id LIMIT 1`, scope.PersonID, scope.InstallationID, scope.ApplicationID).Scan(&credential, &email)
	if err != nil {
		return nil, err
	}
	return []aw.Row{{Table: aw.Persons, ID: scope.PersonID, Access: aw.ExistingUpdate}, {Table: aw.Credentials, ID: credential, Access: aw.ExistingUpdate}, {Table: aw.Emails, ID: email, Access: aw.ExistingUpdate}}, nil
}
func mfaPolicyRows(ctx context.Context, tx *sql.Tx, scope Scope) (result []aw.Row, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT w.id FROM public.workspaces w WHERE w.installation_id=$1 AND w.application_id=$2 AND (w.personal_owner_id=$3 OR EXISTS(SELECT 1 FROM public.workspace_memberships m WHERE m.workspace_id=w.id AND m.person_id=$3 AND m.installation_id=$1 AND m.application_id=$2))`, scope.InstallationID, scope.ApplicationID, scope.PersonID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := rows.Close(); err == nil {
			err = e
		}
	}()
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, aw.Row{Table: aw.Workspaces, ID: id, Access: aw.ExistingUpdate})
	}
	return result, rows.Err()
}
func mfaClock(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var at time.Time
	err := tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&at)
	return at, err
}
func (s *Service) policy(ctx context.Context, tx *sql.Tx, p identity.Principal, action string) error {
	if err := s.cfg.Policy.Ready(ctx, tx); err != nil {
		return ErrUnavailable
	}
	if err := s.cfg.Policy.AuthorizeMFA(ctx, tx, p, action); err != nil {
		if errors.Is(err, ErrDenied) {
			return ErrDenied
		}
		return ErrUnavailable
	}
	return nil
}
func policyOutcome(a *aw.Attempt, err error) aw.Outcome {
	if errors.Is(err, ErrDenied) {
		return mfaFinish(a, aw.DeniedRollback)
	}
	return mfaFinish(a, aw.UnavailableRollback)
}

// checkPrimaryFinal compares the complete original tuple under the acquired
// rows. It neither verifies a password again nor refreshes its original V.
func checkPrimaryFinal(ctx context.Context, tx *sql.Tx, check wp.PasswordCheck) error {
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.identity_persons p JOIN public.identity_credentials c ON c.person_id=p.id JOIN public.identity_emails e ON e.person_id=p.id WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active' AND p.security_epoch=$4 AND c.id=$5 AND c.method='email_password' AND c.revoked_at IS NULL AND c.verifier_hash=$6 AND e.id=$7 AND e.installation_id=$2 AND e.application_id=$3 AND e.comparison_key=$8 AND e.verified_at=$9)`, check.Subject.Person, check.Subject.Realm.Installation, check.Subject.Realm.Application, check.Subject.Epoch, check.CredentialID, check.VerifiedHash, check.Contact.ID, check.Contact.ComparisonKey, check.Contact.VerifiedAt).Scan(&matches)
	if err != nil {
		return ErrUnavailable
	}
	if !matches {
		return ErrDenied
	}
	return nil
}

// checkActorFinal is a read-only comparison of the exact admitted actor. A
// successful rotation may revoke that row; it may not change any other bound.
func checkActorFinal(ctx context.Context, tx *sql.Tx, actor wp.ActorCheck, revoked bool, final time.Time) error {
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.identity_sessions s JOIN public.identity_persons p ON p.id=s.person_id AND p.installation_id=s.installation_id AND p.application_id=s.application_id WHERE s.id=$1 AND s.person_id=$2 AND s.installation_id=$3 AND s.application_id=$4 AND s.environment_id=$5 AND s.token_digest=$6 AND s.security_epoch=$7 AND p.security_epoch=$7 AND p.state='active' AND s.authentication_method=$8 AND s.authenticated_at=$9 AND s.idle_expires_at=$10 AND s.expires_at=$11 AND (s.revoked_at IS NOT NULL)=$12 AND (CASE WHEN s.assurance_expires_at IS NOT NULL AND s.assurance_expires_at<=$15 THEN 'aal1' ELSE s.assurance_level END)=$13 AND (CASE WHEN s.assurance_expires_at IS NULL OR s.assurance_expires_at<=$15 THEN s.expires_at ELSE s.assurance_expires_at END)=$14)`, actor.SessionID, actor.Subject.Person, actor.Subject.Realm.Installation, actor.Subject.Realm.Application, actor.Subject.Realm.Environment, actor.Digest[:], actor.Subject.Epoch, actor.Method, actor.AuthenticatedAt, actor.IdleUntil, actor.AbsoluteUntil, revoked, actor.Assurance, actor.AssuranceUntil, final).Scan(&matches)
	if err != nil {
		return ErrUnavailable
	}
	if !matches {
		return ErrDenied
	}
	return nil
}
