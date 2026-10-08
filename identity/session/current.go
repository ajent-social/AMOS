package session

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

type currentKey struct{}
type currentAdmission struct {
	service   *Service
	session   uuid.UUID
	principal identity.Principal
	digest    [32]byte
	done      <-chan struct{}
	deadline  time.Time
}

func (s *Service) admitCurrent(ctx context.Context, id uuid.UUID, p identity.Principal, digest []byte) context.Context {
	end, ok := ctx.Deadline()
	if !ok || len(digest) != 32 {
		return ctx
	}
	return context.WithValue(ctx, currentKey{}, &currentAdmission{service: s, session: id, principal: p, digest: [32]byte(digest), done: ctx.Done(), deadline: end})
}
func (s *Service) current(ctx context.Context) (*currentAdmission, error) {
	if s == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	v, ok := ctx.Value(currentKey{}).(*currentAdmission)
	if !ok || v == nil || v.service != s || !validID(v.session) {
		return nil, ErrUnauthenticated
	}
	if p, ok := identity.PrincipalFromContext(ctx); ok && p != v.principal {
		return nil, ErrUnauthenticated
	}
	select {
	case <-v.done:
		return nil, ErrUnavailable
	default:
	}
	end, ok := ctx.Deadline()
	if !ok || end.After(v.deadline) || !time.Now().Before(v.deadline) {
		return nil, ErrUnavailable
	}
	return v, nil
}

type currentRow struct {
	person, installation, application, environment  uuid.UUID
	epoch                                           int64
	method                                          string
	authenticated, issued, idle, absolute, lastSeen time.Time
	revoked                                         sql.NullTime
	digest                                          []byte
	level                                           string
	elevated                                        sql.NullTime
}

func (s *Service) readCurrent(ctx context.Context, tx *sql.Tx, person, id uuid.UUID, locking bool) (currentRow, time.Time, error) {
	var v currentRow
	suffix := ""
	if locking {
		suffix = " FOR SHARE"
	}
	var state string
	var epoch int64
	err := tx.QueryRowContext(ctx, `SELECT state,security_epoch FROM public.identity_persons WHERE id=$1 AND installation_id=$2 AND application_id=$3`+suffix, person, s.cfg.InstallationID, s.cfg.ApplicationID).Scan(&state, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return v, time.Time{}, ErrUnauthenticated
	}
	if err != nil {
		return v, time.Time{}, ErrUnavailable
	}
	columns := `person_id,installation_id,application_id,environment_id,security_epoch,authentication_method,authenticated_at,issued_at,idle_expires_at,expires_at,last_seen_at,revoked_at,token_digest`
	args := []any{&v.person, &v.installation, &v.application, &v.environment, &v.epoch, &v.method, &v.authenticated, &v.issued, &v.idle, &v.absolute, &v.lastSeen, &v.revoked, &v.digest}
	v.level = "aal1"
	if s.cfg.PersistAssurance {
		columns += `,assurance_level,assurance_expires_at`
		args = append(args, &v.level, &v.elevated)
	}
	err = tx.QueryRowContext(ctx, `SELECT `+columns+` FROM public.identity_sessions WHERE id=$1 AND person_id=$2 AND installation_id=$3 AND application_id=$4 AND environment_id=$5`+suffix, id, person, s.cfg.InstallationID, s.cfg.ApplicationID, s.cfg.EnvironmentID).Scan(args...)
	if errors.Is(err, sql.ErrNoRows) {
		return v, time.Time{}, ErrUnauthenticated
	}
	if err != nil {
		return v, time.Time{}, ErrUnavailable
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); err != nil || now.IsZero() {
		return v, time.Time{}, ErrUnavailable
	}
	switch state {
	case "active", "pending_verification", "self_disabled", "administratively_disabled", "deletion_pending":
	default:
		return v, now, ErrUnavailable
	}
	if epoch < 0 || v.epoch < 0 || len(v.digest) != 32 || v.authenticated.IsZero() || v.issued.Before(v.authenticated) || !v.absolute.After(v.issued) || v.absolute.After(v.authenticated.Add(maxAge)) || v.lastSeen.Before(v.issued) || !v.idle.After(v.lastSeen) || v.idle.After(v.absolute) || v.idle.After(v.lastSeen.Add(30*time.Minute)) || now.Before(v.lastSeen) {
		return v, now, ErrUnavailable
	}
	if !sessionMethod(v.method) || (v.revoked.Valid && (v.revoked.Time.IsZero() || v.revoked.Time.Before(v.issued))) {
		return v, now, ErrUnavailable
	}
	if _, err = authproof.NewVerifiedCredential(v.person, v.installation, v.application, v.environment, v.epoch, v.method, v.authenticated, "aal1", v.absolute); err != nil {
		return v, now, ErrUnavailable
	}
	if err = validDurableAssurance(v); err != nil {
		return v, now, err
	}
	if state != "active" || epoch != v.epoch || v.revoked.Valid || !now.Before(v.idle) || !now.Before(v.absolute) {
		return v, now, ErrUnauthenticated
	}
	return v, now, nil
}
func validDurableAssurance(v currentRow) error {
	switch v.level {
	case "aal1":
		if v.elevated.Valid {
			return ErrUnavailable
		}
	case "aal2", "aal3":
		if !v.elevated.Valid || !v.elevated.Time.After(v.authenticated) || v.elevated.Time.After(v.absolute) || v.elevated.Time.After(v.authenticated.Add(15*time.Minute)) {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	return nil
}
func currentAssurance(p identity.Principal, v currentRow, now time.Time) (string, time.Time, error) {
	a := p.Assurance()
	level := string(a.Level())
	until := a.ExpiresAt()
	if level != "aal1" && level != "aal2" && level != "aal3" {
		return "", time.Time{}, ErrUnavailable
	}
	if until.IsZero() || !until.After(p.AuthenticatedAt()) || until.After(p.AuthenticatedAt().Add(maxAge)) || (level != "aal1" && until.After(p.AuthenticatedAt().Add(15*time.Minute))) {
		return "", time.Time{}, ErrUnavailable
	}
	if level == "aal1" {
		if v.absolute.Before(until) {
			until = v.absolute
		}
		if !now.Before(until) {
			return "", time.Time{}, ErrUnauthenticated
		}
		return level, until, nil
	}
	if !now.Before(until) || v.level == "aal1" || !now.Before(v.elevated.Time) {
		return "aal1", v.absolute, nil
	}
	if v.level < level {
		level = v.level
	}
	if v.elevated.Time.Before(until) {
		until = v.elevated.Time
	}
	if v.absolute.Before(until) {
		until = v.absolute
	}
	return level, until, nil
}
func matchesCurrent(p identity.Principal, v currentRow) bool {
	return p.PersonID() == v.person && p.InstallationID() == v.installation && p.ApplicationID() == v.application && p.EnvironmentID() == v.environment && p.SecurityEpoch() == v.epoch && p.AuthenticationMethod() == v.method && p.AuthenticatedAt().Equal(v.authenticated)
}
func freshPrincipal(v currentRow, level string, expiry time.Time) (identity.Principal, error) {
	proof, err := authproof.NewVerifiedCredential(v.person, v.installation, v.application, v.environment, v.epoch, v.method, v.authenticated, level, expiry)
	if err != nil {
		return identity.Principal{}, ErrUnavailable
	}
	p, ok := identity.PrincipalFromContext(identity.ContextWithVerifiedCredential(context.Background(), proof))
	if !ok {
		return identity.Principal{}, ErrUnavailable
	}
	return p, nil
}

// RecheckCurrentTx reads current authority under P SHARE then S SHARE. The
// private composition, not this raw transaction argument, attests DB custody.
func (s *Service) RecheckCurrentTx(ctx context.Context, tx *sql.Tx) (identity.Principal, error) {
	if tx == nil {
		return identity.Principal{}, ErrUnavailable
	}
	admit, err := s.current(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	var isolation, readOnly string
	if err = tx.QueryRowContext(ctx, `SELECT pg_catalog.current_setting('transaction_isolation'),pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil || isolation != "read committed" || readOnly != "off" {
		return identity.Principal{}, ErrUnavailable
	}
	v, now, err := s.readCurrent(ctx, tx, admit.principal.PersonID(), admit.session, true)
	if err != nil {
		return identity.Principal{}, err
	}
	if !matchesCurrent(admit.principal, v) || !bytes.Equal(v.digest, admit.digest[:]) {
		return identity.Principal{}, ErrUnauthenticated
	}
	level, expiry, err := currentAssurance(admit.principal, v, now)
	if err != nil {
		return identity.Principal{}, err
	}
	if _, err = s.current(ctx); err != nil {
		return identity.Principal{}, err
	}
	return freshPrincipal(v, level, expiry)
}
func (s *Service) ActorForWriter(ctx context.Context, a *aw.Attempt, request WriterRequest) (wp.ActorCheck, error) {
	d, err := s.writerRequest(ctx, request)
	if err != nil || d.admission == nil || d.attempt != a {
		return wp.ActorCheck{}, ErrUnauthenticated
	}
	rows := []aw.Row{{Table: aw.Persons, ID: d.admission.principal.PersonID(), Access: aw.ExistingUpdate}, {Table: aw.Sessions, ID: d.admission.session, Access: aw.ExistingUpdate}}
	tx, err := a.ParticipantTx(ctx, aw.S, rows)
	if err != nil {
		return wp.ActorCheck{}, ErrUnavailable
	}
	v, now, err := s.readCurrent(ctx, tx, d.admission.principal.PersonID(), d.admission.session, false)
	if err != nil {
		return wp.ActorCheck{}, err
	}
	if !matchesCurrent(d.admission.principal, v) || !bytes.Equal(v.digest, d.admission.digest[:]) || (len(d.digest) > 0 && !bytes.Equal(d.digest, v.digest)) {
		return wp.ActorCheck{}, ErrUnauthenticated
	}
	level, until, err := currentAssurance(d.admission.principal, v, now)
	if err != nil {
		return wp.ActorCheck{}, err
	}
	return wp.ActorCheck{Subject: wp.Subject{Person: v.person, Realm: aw.Realm{Installation: v.installation, Application: v.application, Environment: v.environment}, Epoch: v.epoch}, SessionID: d.admission.session, Digest: [32]byte(v.digest), Method: v.method, AuthenticatedAt: v.authenticated, IdleUntil: v.idle, AbsoluteUntil: v.absolute, Assurance: level, AssuranceUntil: until}, nil
}

func credentialForPrincipal(p identity.Principal) (authproof.VerifiedCredential, error) {
	return authproof.NewVerifiedCredential(p.PersonID(), p.InstallationID(), p.ApplicationID(), p.EnvironmentID(), p.SecurityEpoch(), p.AuthenticationMethod(), p.AuthenticatedAt(), string(p.Assurance().Level()), p.Assurance().ExpiresAt())
}

func sessionMethod(method string) bool {
	switch method {
	case "email_password", "email_magic_link", "google", "github", "apple", "passkey", "enterprise_oidc", "password+totp":
		return true
	}
	return false
}
