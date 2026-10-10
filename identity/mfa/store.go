package mfa

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidFactor = errors.New("invalid MFA factor input")
	ErrFactorAbsent  = errors.New("MFA factor unavailable")
	ErrFactorExists  = errors.New("MFA factor already exists")
	ErrFactorStore   = errors.New("MFA factor persistence unavailable")
)

//go:embed schema.sql
var schemaFS embed.FS

// Fragment contributes the MFA schema at the globally assigned sequence.
func Fragment(sequence uint64) (migrations.Fragment, error) {
	if sequence == 0 {
		return migrations.Fragment{}, migrations.ErrInvalidRegistry
	}
	sqlBytes, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return migrations.Fragment{}, err
	}
	return migrations.Fragment{Namespace: "identity_mfa", Migrations: []migrations.Migration{{
		Sequence: sequence, Namespace: "identity_mfa", Name: "totp_factors", SQL: string(sqlBytes),
	}}}, nil
}

type FactorState string

const (
	FactorPending FactorState = "pending"
	FactorActive  FactorState = "active"
	FactorExpired FactorState = "expired"
)

type Scope struct {
	InstallationID uuid.UUID
	ApplicationID  uuid.UUID
	EnvironmentID  uuid.UUID
	PersonID       uuid.UUID
}

type Factor struct {
	ID                   uuid.UUID
	Scope                Scope
	SeedCiphertext       []byte
	State                FactorState
	PendingSecurityEpoch *int64
	LastUsedStep         int64
	FailedAttempts       int
	LockedUntil          *time.Time
	CreatedAt            time.Time
	ExpiresAt            *time.Time
	ActivatedAt          *time.Time
}

// Store only uses the caller's transaction; factor state and session changes
// can therefore be committed as one security transition.
type Store struct {
	tx      *sql.Tx
	attempt *aw.Attempt
}

func NewStore(tx *sql.Tx) (*Store, error) {
	if aw.SelectLegacy() != nil || tx == nil {
		return nil, ErrInvalidFactor
	}
	return &Store{tx: tx}, nil
}

func (s *Store) createPending(ctx context.Context, factor Factor, securityEpoch int64, now time.Time) error {
	if s == nil || s.tx == nil || ctx == nil || !validScope(factor.Scope) || !validID(factor.ID) || len(factor.SeedCiphertext) < 32 || len(factor.SeedCiphertext) > 4096 || securityEpoch < 0 || now.IsZero() || factor.State != FactorPending {
		return ErrInvalidFactor
	}
	var expired int
	if err := s.tx.QueryRowContext(ctx, `UPDATE identity_totp_factors SET state='expired',pending_security_epoch=NULL,expires_at=NULL,updated_at=$5 WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND person_id=$4 AND state='pending' AND expires_at<=$5 RETURNING 1`, factor.Scope.InstallationID, factor.Scope.ApplicationID, factor.Scope.EnvironmentID, factor.Scope.PersonID, now.UTC()).Scan(&expired); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrFactorStore
	}
	return s.insertPending(ctx, factor, securityEpoch, now)
}

func (s *Store) insertPending(ctx context.Context, factor Factor, securityEpoch int64, now time.Time) error {
	result, err := s.tx.ExecContext(ctx, `INSERT INTO identity_totp_factors(id,installation_id,application_id,environment_id,person_id,seed_ciphertext,seed_format,state,pending_security_epoch,last_used_step,failed_attempts,created_at,expires_at)
		SELECT $1,$2,$3,$4,$5,$6,1,'pending',$7,-1,0,$8,$9
		WHERE EXISTS(SELECT 1 FROM identity_persons p WHERE p.id=$5 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active' AND p.security_epoch=$7)
		AND NOT EXISTS(SELECT 1 FROM identity_totp_factors f WHERE f.installation_id=$2 AND f.application_id=$3 AND f.environment_id=$4 AND f.person_id=$5 AND f.state='active')`, factor.ID, factor.Scope.InstallationID, factor.Scope.ApplicationID, factor.Scope.EnvironmentID, factor.Scope.PersonID, factor.SeedCiphertext, securityEpoch, now.UTC(), now.UTC().Add(DefaultPendingLifetime))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrFactorExists
		}
		return ErrFactorStore
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrFactorStore
	}
	if rows != 1 {
		return ErrFactorExists
	}
	return nil
}

func (s *Store) find(ctx context.Context, scope Scope, id uuid.UUID) (Factor, error) {
	if s == nil || s.tx == nil || ctx == nil || !validScope(scope) || !validID(id) {
		return Factor{}, ErrInvalidFactor
	}
	var f Factor
	f.Scope = scope
	var state string
	var pendingEpoch sql.NullInt64
	var lockedUntil, expiresAt, activatedAt sql.NullTime
	err := s.tx.QueryRowContext(ctx, `SELECT f.id,f.seed_ciphertext,f.state,f.pending_security_epoch,f.last_used_step,f.failed_attempts,f.locked_until,f.created_at,f.expires_at,f.activated_at
		FROM identity_totp_factors f JOIN identity_persons p ON (p.id,p.installation_id,p.application_id)=(f.person_id,f.installation_id,f.application_id)
		WHERE f.id=$1 AND f.installation_id=$2 AND f.application_id=$3 AND f.environment_id=$4 AND f.person_id=$5 AND p.state='active' AND f.state IN ('pending','active')`, id, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID).Scan(&f.ID, &f.SeedCiphertext, &state, &pendingEpoch, &f.LastUsedStep, &f.FailedAttempts, &lockedUntil, &f.CreatedAt, &expiresAt, &activatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Factor{}, ErrFactorAbsent
	}
	if err != nil {
		return Factor{}, ErrFactorStore
	}
	f.State = FactorState(state)
	if pendingEpoch.Valid {
		f.PendingSecurityEpoch = &pendingEpoch.Int64
	}
	if lockedUntil.Valid {
		f.LockedUntil = &lockedUntil.Time
	}
	if expiresAt.Valid {
		f.ExpiresAt = &expiresAt.Time
	}
	if activatedAt.Valid {
		f.ActivatedAt = &activatedAt.Time
	}
	return f, nil
}

func (s *Store) findCurrent(ctx context.Context, scope Scope, state FactorState) (Factor, error) {
	if state != FactorPending && state != FactorActive {
		return Factor{}, ErrInvalidFactor
	}
	if s == nil || s.tx == nil || ctx == nil || !validScope(scope) {
		return Factor{}, ErrInvalidFactor
	}
	var f Factor
	f.Scope = scope
	var pendingEpoch sql.NullInt64
	var lockedUntil, expiresAt, activatedAt sql.NullTime
	err := s.tx.QueryRowContext(ctx, `SELECT f.id,f.seed_ciphertext,f.state,f.pending_security_epoch,f.last_used_step,f.failed_attempts,f.locked_until,f.created_at,f.expires_at,f.activated_at
		FROM identity_totp_factors f JOIN identity_persons p ON (p.id,p.installation_id,p.application_id)=(f.person_id,f.installation_id,f.application_id)
		WHERE f.installation_id=$1 AND f.application_id=$2 AND f.environment_id=$3 AND f.person_id=$4 AND f.state=$5 AND p.state='active' ORDER BY f.created_at DESC LIMIT 1`, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID, string(state)).Scan(&f.ID, &f.SeedCiphertext, &f.State, &pendingEpoch, &f.LastUsedStep, &f.FailedAttempts, &lockedUntil, &f.CreatedAt, &expiresAt, &activatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Factor{}, ErrFactorAbsent
	}
	if err != nil {
		return Factor{}, ErrFactorStore
	}
	if pendingEpoch.Valid {
		f.PendingSecurityEpoch = &pendingEpoch.Int64
	}
	if lockedUntil.Valid {
		f.LockedUntil = &lockedUntil.Time
	}
	if expiresAt.Valid {
		f.ExpiresAt = &expiresAt.Time
	}
	if activatedAt.Valid {
		f.ActivatedAt = &activatedAt.Time
	}
	return f, nil
}

func (s *Store) activatePendingAndConsumeStep(ctx context.Context, scope Scope, id uuid.UUID, securityEpoch, step int64, activeCiphertext []byte, now time.Time) error {
	if s == nil || s.tx == nil || ctx == nil || !validScope(scope) || !validID(id) || securityEpoch < 0 || step < 0 || len(activeCiphertext) < 32 || len(activeCiphertext) > 4096 || now.IsZero() {
		return ErrInvalidFactor
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE identity_totp_factors f SET seed_ciphertext=$6,state='active',pending_security_epoch=NULL,last_used_step=$7,failed_attempts=0,locked_until=NULL,expires_at=NULL,activated_at=$8,updated_at=$8
		WHERE f.id=$1 AND f.installation_id=$2 AND f.application_id=$3 AND f.environment_id=$4 AND f.person_id=$5 AND f.state='pending' AND f.pending_security_epoch=$9 AND f.expires_at>$8
		AND EXISTS(SELECT 1 FROM identity_persons p WHERE p.id=$5 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active' AND p.security_epoch=$9)
		AND NOT EXISTS(SELECT 1 FROM identity_totp_factors a WHERE a.installation_id=$2 AND a.application_id=$3 AND a.environment_id=$4 AND a.person_id=$5 AND a.state='active')`, id, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID, activeCiphertext, step, now.UTC(), securityEpoch)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrFactorExists
		}
		return ErrFactorStore
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrFactorStore
	}
	if rows != 1 {
		return ErrFactorAbsent
	}
	return nil
}

// AcceptStep atomically consumes an active factor's accepted RFC 6238 step.
func (s *Store) acceptStep(ctx context.Context, scope Scope, id uuid.UUID, step int64, now time.Time) (bool, error) {
	if s == nil || s.tx == nil || ctx == nil || !validScope(scope) || !validID(id) || step < 0 || now.IsZero() {
		return false, ErrInvalidFactor
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE identity_totp_factors f SET last_used_step=$6,failed_attempts=0,locked_until=NULL,updated_at=$7
		WHERE f.id=$1 AND f.installation_id=$2 AND f.application_id=$3 AND f.environment_id=$4 AND f.person_id=$5 AND f.state='active' AND f.last_used_step<$6 AND (f.locked_until IS NULL OR f.locked_until<=$7)
		AND EXISTS(SELECT 1 FROM identity_persons p WHERE p.id=$5 AND p.installation_id=$2 AND p.application_id=$3 AND p.state='active')`, id, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID, step, now.UTC())
	if err != nil {
		return false, ErrFactorStore
	}
	rows, err := result.RowsAffected()
	return rows == 1, mapRowsError(err)
}

func (s *Store) recordFailure(ctx context.Context, scope Scope, id uuid.UUID, now time.Time) error {
	if s == nil || s.tx == nil || ctx == nil || !validScope(scope) || !validID(id) || now.IsZero() {
		return ErrInvalidFactor
	}
	_, err := s.tx.ExecContext(ctx, `UPDATE identity_totp_factors SET failed_attempts=CASE WHEN locked_until IS NOT NULL AND locked_until<=$6 THEN 1 ELSE LEAST(failed_attempts+1,$7) END,
		locked_until=CASE WHEN locked_until IS NOT NULL AND locked_until>$6 THEN locked_until WHEN (CASE WHEN locked_until IS NOT NULL AND locked_until<=$6 THEN 1 ELSE failed_attempts+1 END)>=$7 THEN $6+($8*interval '1 microsecond') ELSE NULL END,updated_at=$6
		WHERE id=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4 AND person_id=$5 AND state IN ('pending','active') AND (locked_until IS NULL OR locked_until<=$6)`, id, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID, scope.PersonID, now.UTC(), DefaultMaxAttempts, int64(DefaultCodeLockout/time.Microsecond))
	if err != nil {
		return ErrFactorStore
	}
	return nil
}

func (s *Store) Locked(f Factor, now time.Time) bool {
	return f.LockedUntil != nil && f.LockedUntil.After(now)
}

func validScope(s Scope) bool {
	return validID(s.InstallationID) && validID(s.ApplicationID) && validID(s.EnvironmentID) && validID(s.PersonID)
}
func validID(id uuid.UUID) bool {
	return id != uuid.Nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
}
func mapRowsError(err error) error {
	if err != nil {
		return ErrFactorStore
	}
	return nil
}
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
