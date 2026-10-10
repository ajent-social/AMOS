package store

import (
	"context"
	"database/sql"
	"errors"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// existing authorizes the complete sealed existing inventory before any plain
// lookup. It never invents a row from a caller's digest or changes a plan.
func (s *Store) existing(ctx context.Context, table aw.Table, phase aw.Phase) (*sql.Tx, []aw.Row, error) {
	rows, err := s.attempt.PlannedRows(table)
	if err != nil {
		return nil, nil, s.failed(ErrPersistence)
	}
	existing := make([]aw.Row, 0, len(rows))
	for _, r := range rows {
		if r.Access == aw.ExistingUpdate {
			existing = append(existing, r)
		}
	}
	if len(existing) == 0 {
		return nil, nil, ErrSessionUnavailable
	}
	tx, err := s.attempt.ParticipantTx(ctx, phase, existing)
	if err != nil {
		return nil, nil, s.failed(ErrPersistence)
	}
	return tx, existing, nil
}
func (s *Store) sessionTarget(ctx context.Context, digest []byte, scope *SessionScope) (uuid.UUID, error) {
	if len(digest) != 32 {
		return uuid.Nil, s.failed(ErrInvalidInput)
	}
	tx, _, err := s.existing(ctx, aw.Sessions, aw.S)
	if err != nil {
		return uuid.Nil, err
	}
	realm, err := s.attempt.Realm()
	if err != nil {
		return uuid.Nil, s.failed(ErrPersistence)
	}
	if scope != nil && (scope.InstallationID != realm.Installation || scope.ApplicationID != realm.Application || scope.EnvironmentID != realm.Environment) {
		return uuid.Nil, s.failed(ErrPersistence)
	}
	query := `SELECT id FROM public.identity_sessions WHERE token_digest=$1 AND installation_id=$2 AND application_id=$3`
	args := []any{digest, realm.Installation, realm.Application}
	if scope != nil {
		query += ` AND environment_id=$4`
		args = append(args, realm.Environment)
	}
	var id uuid.UUID
	err = tx.QueryRowContext(ctx, query, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, ErrSessionUnavailable
	}
	if err != nil {
		return uuid.Nil, s.failed(ErrPersistence)
	}
	if _, err = s.attempt.ParticipantTx(ctx, aw.S, []aw.Row{row(aw.Sessions, id, aw.ExistingUpdate)}); err != nil {
		return uuid.Nil, s.failed(ErrPersistence)
	}
	return id, nil
}
func (s *Store) RevokeSession(ctx context.Context, digest []byte) error {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return e
		}
		return p.revokeSession(ctx, digest)
	}
	id, e := s.sessionTarget(ctx, digest, nil)
	if e != nil {
		return e
	}
	rows := []aw.Row{row(aw.Sessions, id, aw.ExistingUpdate)}
	p, e := s.participant(ctx, aw.S, rows, aw.SessionWrite, rows)
	if e != nil {
		return e
	}
	return s.failed(p.revokeSession(ctx, digest))
}
func (s *Store) RevokeSessionScoped(ctx context.Context, digest []byte, scope SessionScope) error {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return e
		}
		return p.revokeSessionScoped(ctx, digest, scope)
	}
	id, e := s.sessionTarget(ctx, digest, &scope)
	if e != nil {
		return e
	}
	rows := []aw.Row{row(aw.Sessions, id, aw.ExistingUpdate)}
	p, e := s.participant(ctx, aw.S, rows, aw.SessionWrite, rows)
	if e != nil {
		return e
	}
	return s.failed(p.revokeSessionScoped(ctx, digest, scope))
}
func (s *Store) FindActiveSession(ctx context.Context, digest []byte, scope SessionScope) (AuthenticatedSession, error) {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return AuthenticatedSession{}, e
		}
		return p.findActiveSession(ctx, digest, scope)
	}
	id, e := s.sessionTarget(ctx, digest, &scope)
	if e != nil {
		return AuthenticatedSession{}, e
	}
	tx, _, e := s.existing(ctx, aw.Persons, aw.P)
	if e != nil {
		return AuthenticatedSession{}, e
	}
	var person uuid.UUID
	e = tx.QueryRowContext(ctx, `SELECT person_id FROM public.identity_sessions WHERE id=$1`, id).Scan(&person)
	if e != nil {
		return AuthenticatedSession{}, s.failed(ErrPersistence)
	}
	writes := []aw.Row{row(aw.Sessions, id, aw.ExistingUpdate)}
	rows := append([]aw.Row{row(aw.Persons, person, aw.ExistingUpdate)}, writes...)
	p, e := s.participant(ctx, aw.S, rows, aw.SessionWrite, writes)
	if e != nil {
		return AuthenticatedSession{}, e
	}
	v, e := p.findActiveSession(ctx, digest, scope)
	return v, s.failed(e)
}
func (s *Store) FindCurrentPassword(ctx context.Context, scope SessionScope, person uuid.UUID, epoch int64) (string, error) {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return "", e
		}
		return p.findCurrentPassword(ctx, scope, person, epoch)
	}
	if e := s.scope(scope.InstallationID, scope.ApplicationID, scope.EnvironmentID); e != nil {
		return "", e
	}
	tx, _, e := s.existing(ctx, aw.Credentials, aw.C)
	if e != nil {
		return "", e
	}
	if _, e = s.attempt.ParticipantTx(ctx, aw.C, []aw.Row{row(aw.Persons, person, aw.ExistingUpdate)}); e != nil {
		return "", s.failed(ErrPersistence)
	}
	if _, _, e = s.existing(ctx, aw.Emails, aw.C); e != nil {
		return "", e
	}
	var credential, contact uuid.UUID
	var encoded string
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.verifier_hash,e.id FROM public.identity_persons p JOIN public.identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL JOIN public.identity_emails e ON e.person_id=p.id AND e.installation_id=p.installation_id AND e.application_id=p.application_id AND e.verified_at IS NOT NULL WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.security_epoch=$4 AND p.state='active' ORDER BY e.id LIMIT 1`, person, scope.InstallationID, scope.ApplicationID, epoch).Scan(&credential, &encoded, &contact)
	if errors.Is(e, sql.ErrNoRows) {
		return "", ErrSessionUnavailable
	}
	if e != nil {
		return "", s.failed(ErrPersistence)
	}
	if _, e = s.attempt.ParticipantTx(ctx, aw.C, []aw.Row{row(aw.Credentials, credential, aw.ExistingUpdate), row(aw.Emails, contact, aw.ExistingUpdate)}); e != nil {
		return "", s.failed(ErrPersistence)
	}
	return encoded, nil
}
func (s *Store) CreateMagicChallenge(ctx context.Context, v Challenge, digest []byte) error {
	if s == nil || s.attempt == nil {
		p, e := s.legacy(ctx)
		if e != nil {
			return e
		}
		return p.createMagicChallenge(ctx, v, digest)
	}
	if v.Purpose != ChallengeEmailMagicLink || len(digest) != 32 || len(v.Digest) != 32 || !validID(v.ID) || !validID(v.PersonID) || !validID(v.EmailID) {
		return s.failed(ErrInvalidInput)
	}
	writes := []aw.Row{row(aw.Challenges, v.ID, aw.ReservedInsert)}
	rows := append([]aw.Row{row(aw.Persons, v.PersonID, aw.ExistingUpdate), row(aw.Emails, v.EmailID, aw.ExistingUpdate)}, writes...)
	p, e := s.participant(ctx, aw.H, rows, aw.ChallengeWrite, writes)
	if e != nil {
		return e
	}
	// The binding is part of this single insert, not a second unjournaled update.
	if p.startedAt == nil || !v.ExpiresAt.After(*p.startedAt) {
		return s.failed(ErrInvalidInput)
	}
	result, e := p.tx.ExecContext(ctx, `INSERT INTO public.identity_challenges(id,person_id,email_id,purpose,token_digest,expires_at,browser_binding_digest,created_at) SELECT $1,e.person_id,e.id,$4,$5,$6::timestamptz,$7,$8::timestamptz FROM public.identity_emails e WHERE e.id=$3 AND e.person_id=$2 AND $6::timestamptz>$8::timestamptz`, v.ID, v.PersonID, v.EmailID, v.Purpose, v.Digest, v.ExpiresAt, digest, p.startedAt)
	return s.failed(dbResult(result, e))
}
