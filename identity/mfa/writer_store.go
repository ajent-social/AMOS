package mfa

import (
	"context"
	"database/sql"
	"errors"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// NewWriter retains the exact attempt. Every operation independently checks its
// acquired person/factor rows before obtaining SQL; construction grants no proof.
func NewWriter(a *aw.Attempt) (*Store, error) {
	if _, err := a.Binding(); err != nil {
		return nil, ErrFactorStore
	}
	return &Store{attempt: a}, nil
}

// ReadStore exposes only plain reads. It cannot be promoted to a writer store.
// Its caller owns the lexical Root.Read transaction and session recheck.
type ReadStore struct{ tx *sql.Tx }

func NewReadStore(tx *sql.Tx) (*ReadStore, error) {
	if tx == nil {
		return nil, ErrInvalidFactor
	}
	return &ReadStore{tx: tx}, nil
}
func (s *ReadStore) Find(ctx context.Context, scope Scope, id uuid.UUID) (Factor, error) {
	if s == nil {
		return Factor{}, ErrInvalidFactor
	}
	return (&Store{tx: s.tx}).find(ctx, scope, id)
}
func (s *ReadStore) FindCurrent(ctx context.Context, scope Scope, state FactorState) (Factor, error) {
	if s == nil {
		return Factor{}, ErrInvalidFactor
	}
	return (&Store{tx: s.tx}).findCurrent(ctx, scope, state)
}
func factorRow(id uuid.UUID, access aw.Access) aw.Row {
	return aw.Row{Table: aw.Factors, ID: id, Access: access}
}
func (s *Store) finishError(err error) error {
	if err == nil || s == nil || s.attempt == nil {
		return err
	}
	outcome := aw.UnavailableRollback
	if errors.Is(err, ErrFactorAbsent) || errors.Is(err, ErrFactorExists) {
		outcome = aw.DeniedRollback
	}
	if e := s.attempt.Finish(outcome); e != nil {
		return ErrFactorStore
	}
	return err
}
func (s *Store) participant(ctx context.Context, scope Scope, rows []aw.Row, kind aw.Mutation) (*Store, error) {
	if s == nil {
		return nil, ErrInvalidFactor
	}
	if ctx == nil || !validScope(scope) {
		return nil, s.finishError(ErrInvalidFactor)
	}
	if s.attempt == nil {
		if aw.SelectLegacy() != nil || s.tx == nil {
			return nil, ErrFactorStore
		}
		return s, nil
	}
	realm, err := s.attempt.Realm()
	if err != nil || realm != (aw.Realm{Installation: scope.InstallationID, Application: scope.ApplicationID, Environment: scope.EnvironmentID}) {
		return nil, s.finishError(ErrFactorStore)
	}
	required := append([]aw.Row{{Table: aw.Persons, ID: scope.PersonID, Access: aw.ExistingUpdate}}, rows...)
	tx, err := s.attempt.ParticipantTx(ctx, aw.H, required)
	if err != nil {
		return nil, s.finishError(ErrFactorStore)
	}
	if kind != 0 {
		if err = s.attempt.RecordMutation(kind, rows); err != nil {
			return nil, s.finishError(ErrFactorStore)
		}
	}
	return &Store{tx: tx}, nil
}
func (s *Store) Find(ctx context.Context, scope Scope, id uuid.UUID) (Factor, error) {
	p, err := s.participant(ctx, scope, []aw.Row{factorRow(id, aw.ExistingUpdate)}, 0)
	if err != nil {
		return Factor{}, err
	}
	f, err := p.find(ctx, scope, id)
	// Absence is a non-mutating domain result; callers decide the root outcome.
	if err != nil && !errors.Is(err, ErrFactorAbsent) {
		return Factor{}, s.finishError(err)
	}
	return f, err
}
func (s *Store) FindCurrent(ctx context.Context, scope Scope, state FactorState) (Factor, error) {
	var rows []aw.Row
	if s != nil && s.attempt != nil {
		var err error
		rows, err = s.attempt.PlannedRows(aw.Factors)
		if err != nil {
			return Factor{}, s.finishError(ErrFactorStore)
		}
		existing := rows[:0]
		for _, r := range rows {
			if r.Access == aw.ExistingUpdate {
				existing = append(existing, r)
			}
		}
		rows = existing
	}
	p, err := s.participant(ctx, scope, rows, 0)
	if err != nil {
		return Factor{}, err
	}
	f, err := p.findCurrent(ctx, scope, state)
	if errors.Is(err, ErrFactorAbsent) {
		return f, err
	}
	if err != nil {
		return Factor{}, s.finishError(err)
	}
	if s.attempt != nil {
		if err = s.attempt.CheckRows(aw.H, []aw.Row{factorRow(f.ID, aw.ExistingUpdate)}); err != nil {
			return Factor{}, s.finishError(ErrFactorStore)
		}
	}
	return f, nil
}
func (s *Store) CreatePending(ctx context.Context, f Factor, epoch int64, at time.Time) error {
	if s == nil || s.attempt == nil {
		p, err := s.participant(ctx, f.Scope, nil, 0)
		if err != nil {
			return err
		}
		return p.createPending(ctx, f, epoch, at)
	}
	if !validID(f.ID) || len(f.SeedCiphertext) < 32 || len(f.SeedCiphertext) > 4096 || epoch < 0 || at.IsZero() || f.State != FactorPending {
		return s.finishError(ErrInvalidFactor)
	}
	p, err := s.participant(ctx, f.Scope, []aw.Row{factorRow(f.ID, aw.ReservedInsert)}, 0)
	if err != nil {
		return err
	}
	// Discover all candidates under G before acquisition. This second plain read
	// checks that every expired row is held before its exact-ID mutation.
	rows, err := p.tx.QueryContext(ctx, `SELECT id FROM public.identity_totp_factors WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND person_id=$4 AND state='pending' AND expires_at<=$5`, f.Scope.InstallationID, f.Scope.ApplicationID, f.Scope.EnvironmentID, f.Scope.PersonID, at)
	if err != nil {
		return s.finishError(ErrFactorStore)
	}
	var expired []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			break
		}
		expired = append(expired, id)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	if e := rows.Close(); err == nil {
		err = e
	}
	if err != nil {
		return s.finishError(ErrFactorStore)
	}
	for _, id := range expired {
		target := []aw.Row{factorRow(id, aw.ExistingUpdate)}
		if _, err = s.participant(ctx, f.Scope, target, aw.FactorSuccessWrite); err != nil {
			return err
		}
		result, e := p.tx.ExecContext(ctx, `UPDATE public.identity_totp_factors SET state='expired',pending_security_epoch=NULL,expires_at=NULL,updated_at=$2 WHERE id=$1 AND state='pending' AND expires_at<=$2`, id, at)
		if e = exactFactorResult(result, e); e != nil {
			return s.finishError(e)
		}
	}
	if _, err = s.participant(ctx, f.Scope, []aw.Row{factorRow(f.ID, aw.ReservedInsert)}, aw.FactorSuccessWrite); err != nil {
		return err
	}
	return s.finishError(p.insertPending(ctx, f, epoch, at))
}
func (s *Store) ActivatePendingAndConsumeStep(ctx context.Context, scope Scope, id uuid.UUID, epoch, step int64, ciphertext []byte, at time.Time) error {
	p, err := s.participant(ctx, scope, []aw.Row{factorRow(id, aw.ExistingUpdate)}, aw.FactorSuccessWrite)
	if err != nil {
		return err
	}
	return s.finishError(p.activatePendingAndConsumeStep(ctx, scope, id, epoch, step, ciphertext, at))
}
func (s *Store) AcceptStep(ctx context.Context, scope Scope, id uuid.UUID, step int64, at time.Time) (bool, error) {
	p, err := s.participant(ctx, scope, []aw.Row{factorRow(id, aw.ExistingUpdate)}, aw.FactorSuccessWrite)
	if err != nil {
		return false, err
	}
	ok, err := p.acceptStep(ctx, scope, id, step, at)
	if err == nil && !ok && s.attempt != nil {
		err = ErrFactorAbsent
	}
	return ok, s.finishError(err)
}
func (s *Store) RecordFailure(ctx context.Context, scope Scope, id uuid.UUID, at time.Time) error {
	p, err := s.participant(ctx, scope, []aw.Row{factorRow(id, aw.ExistingUpdate)}, aw.CounterWrite)
	if err != nil {
		return err
	}
	if s.attempt == nil {
		return p.recordFailure(ctx, scope, id, at)
	}
	if !validID(id) || at.IsZero() {
		return s.finishError(ErrInvalidFactor)
	}
	result, err := p.tx.ExecContext(ctx, `UPDATE public.identity_totp_factors SET failed_attempts=CASE WHEN locked_until IS NOT NULL AND locked_until<=$6 THEN 1 ELSE LEAST(failed_attempts+1,$7) END,
 locked_until=CASE WHEN (CASE WHEN locked_until IS NOT NULL AND locked_until<=$6 THEN 1 ELSE failed_attempts+1 END)>=$7 THEN $6+($8*interval '1 microsecond') ELSE NULL END,updated_at=$6
 WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4 AND person_id=$5 AND state IN ('pending','active') AND (locked_until IS NULL OR locked_until<=$6)`, id, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID, at.UTC(), DefaultMaxAttempts, int64(DefaultCodeLockout/time.Microsecond))
	return s.finishError(exactFactorResult(result, err))
}
func exactFactorResult(result sql.Result, err error) error {
	if err != nil {
		return ErrFactorStore
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrFactorStore
	}
	if n != 1 {
		return ErrFactorAbsent
	}
	return nil
}
