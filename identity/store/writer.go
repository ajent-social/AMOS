package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	wp "github.com/ajent-social/amos/identity/internal/writerproof"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// NewWriter retains a live sealed attempt, never an independently usable tx.
func NewWriter(a *aw.Attempt) (*Store, error) {
	if _, err := a.Binding(); err != nil {
		return nil, ErrPersistence
	}
	return &Store{attempt: a}, nil
}

// NewIssuer checks issuance without consuming the session-owned credential getter.
func NewIssuer(a *aw.Attempt, proof wp.Issuance) (*Store, error) {
	if err := proof.Check(a); err != nil {
		return nil, ErrPersistence
	}
	s, err := NewWriter(a)
	if err != nil {
		return nil, err
	}
	s.issuance = proof
	return s, nil
}

func row(t aw.Table, id uuid.UUID, access aw.Access) aw.Row {
	return aw.Row{Table: t, ID: id, Access: access}
}
func (s *Store) failed(err error) error {
	if err != nil && s != nil && s.attempt != nil {
		outcome := aw.UnavailableRollback
		if errors.Is(err, ErrSessionUnavailable) || errors.Is(err, ErrPersonUnavailable) || errors.Is(err, ErrChallengeUnavailable) {
			outcome = aw.DeniedRollback
		}
		if e := s.attempt.Finish(outcome); e != nil {
			return err
		}
	}
	return err
}
func (s *Store) participant(ctx context.Context, phase aw.Phase, rows []aw.Row, kind aw.Mutation, writes []aw.Row) (*Store, error) {
	if s == nil {
		return nil, ErrInvalidInput
	}
	if ctx == nil {
		return nil, s.failed(ErrInvalidInput)
	}
	if s.attempt == nil {
		if err := aw.SelectLegacy(); err != nil {
			return nil, ErrPersistence
		}
		if s.tx == nil {
			return nil, ErrInvalidInput
		}
		return s, nil
	}
	tx, err := s.attempt.ParticipantTx(ctx, phase, rows)
	if err != nil {
		return nil, s.failed(ErrPersistence)
	}
	if kind != 0 {
		if err = s.attempt.RecordMutation(kind, writes); err != nil {
			return nil, s.failed(ErrPersistence)
		}
	}
	b, err := s.attempt.StartedAt()
	if err != nil {
		return nil, s.failed(ErrPersistence)
	}
	return &Store{tx: tx, startedAt: &b}, nil
}
func (s *Store) issuer() error {
	if s != nil && s.attempt != nil {
		if err := s.issuance.Check(s.attempt); err != nil {
			return s.failed(ErrPersistence)
		}
	}
	return nil
}
func (s *Store) scope(installation, application, environment uuid.UUID) error {
	if s == nil {
		return ErrInvalidInput
	}
	if s.attempt == nil {
		return nil
	}
	r, err := s.attempt.Realm()
	if err != nil || r.Installation != installation || r.Application != application || (environment != uuid.Nil && r.Environment != environment) {
		return s.failed(ErrPersistence)
	}
	return nil
}

func (s *Store) AdvanceSecurityEpoch(ctx context.Context, id uuid.UUID) (int64, error) {
	rows := []aw.Row{row(aw.Persons, id, aw.ExistingUpdate)}
	p, e := s.participant(ctx, aw.P, rows, aw.CredentialWrite, rows)
	if e != nil {
		return 0, e
	}
	v, e := p.advanceSecurityEpoch(ctx, id)
	return v, s.failed(e)
}
func (s *Store) LinkExternalIdentity(ctx context.Context, b ExternalBinding) error {
	writes := []aw.Row{row(aw.Bindings, b.ID, aw.ReservedInsert)}
	rows := append([]aw.Row{row(aw.Persons, b.PersonID, aw.ExistingUpdate), row(aw.Connections, b.ProviderConnectionID, aw.ExistingShare)}, writes...)
	p, e := s.participant(ctx, aw.C, rows, aw.BindingWrite, writes)
	if e != nil {
		return e
	}
	return s.failed(p.linkExternalIdentity(ctx, b))
}
func (s *Store) CreateSession(ctx context.Context, v Session) error {
	if err := sessionAssurance(v, s != nil && s.attempt != nil); err != nil {
		return s.failed(err)
	}
	if e := s.issuer(); e != nil {
		return e
	}
	if e := s.scope(v.InstallationID, v.ApplicationID, v.EnvironmentID); e != nil {
		return e
	}
	writes := []aw.Row{row(aw.Sessions, v.ID, aw.ReservedInsert)}
	rows := append([]aw.Row{row(aw.Persons, v.PersonID, aw.ExistingUpdate)}, writes...)
	p, e := s.participant(ctx, aw.S, rows, aw.SessionWrite, writes)
	if e != nil {
		return e
	}
	return s.failed(p.createSession(ctx, v))
}
func (s *Store) SetSessionAssurance(ctx context.Context, id uuid.UUID, level string, expires time.Time) error {
	if e := s.issuer(); e != nil {
		return e
	}
	access := aw.ReservedInsert
	if s != nil && s.attempt != nil {
		planned, err := s.attempt.PlannedRows(aw.Sessions)
		if err != nil {
			return s.failed(ErrPersistence)
		}
		for _, r := range planned {
			if r.ID == id {
				access = r.Access
				break
			}
		}
	}
	rows := []aw.Row{row(aw.Sessions, id, access)}
	p, e := s.participant(ctx, aw.S, rows, aw.SessionWrite, rows)
	if e != nil {
		return e
	}
	return s.failed(p.setSessionAssurance(ctx, id, level, expires))
}
func (s *Store) ActiveSessionAssurance(ctx context.Context, id uuid.UUID) (string, time.Time, error) {
	rows := []aw.Row{row(aw.Sessions, id, aw.ExistingUpdate)}
	p, e := s.participant(ctx, aw.S, rows, 0, nil)
	if e != nil {
		return "", time.Time{}, e
	}
	v, t, e := p.activeSessionAssurance(ctx, id)
	return v, t, s.failed(e)
}
func (s *Store) CreateChallenge(ctx context.Context, v Challenge) error {
	writes := []aw.Row{row(aw.Challenges, v.ID, aw.ReservedInsert)}
	rows := append([]aw.Row{row(aw.Persons, v.PersonID, aw.ExistingUpdate), row(aw.Emails, v.EmailID, aw.ExistingUpdate)}, writes...)
	p, e := s.participant(ctx, aw.H, rows, aw.ChallengeWrite, writes)
	if e != nil {
		return e
	}
	return s.failed(p.createChallenge(ctx, v))
}
func (s *Store) ConsumeChallenge(ctx context.Context, id uuid.UUID, purpose string, digest []byte) (ConsumedChallenge, error) {
	rows := []aw.Row{row(aw.Challenges, id, aw.ExistingUpdate)}
	p, e := s.participant(ctx, aw.H, rows, aw.ChallengeWrite, rows)
	if e != nil {
		return ConsumedChallenge{}, e
	}
	v, e := p.consumeChallenge(ctx, id, purpose, digest)
	return v, s.failed(e)
}
func (s *Store) MarkEmailVerified(ctx context.Context, person, email uuid.UUID) error {
	writes := []aw.Row{row(aw.Emails, email, aw.ExistingUpdate)}
	rows := append([]aw.Row{row(aw.Persons, person, aw.ExistingUpdate)}, writes...)
	p, e := s.participant(ctx, aw.C, rows, aw.ContactWrite, writes)
	if e != nil {
		return e
	}
	return s.failed(p.markEmailVerified(ctx, person, email))
}

// Legacy methods are never used as a discovery escape after a plan is sealed.
func (s *Store) legacy(ctx context.Context) (*Store, error) {
	if s == nil || s.attempt != nil {
		return nil, ErrPersistence
	}
	return s.participant(ctx, aw.P, nil, 0, nil)
}

// dbResult checks a required single-row mutation without exposing diagnostics.
func dbResult(result sql.Result, err error) error {
	if err != nil {
		return ErrPersistence
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if n != 1 {
		return ErrPersonUnavailable
	}
	return nil
}
