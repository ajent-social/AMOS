// Package sqlstore persists bounded jobs and their execution fencing in PostgreSQL.
package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/ajent-social/amos/jobs"
	"github.com/google/uuid"
)

var (
	ErrInvalidConfig = errors.New("invalid job store configuration")
	ErrUnavailable   = errors.New("job store unavailable")
)

const (
	HardMaxPayloadBytes           = 1 << 20
	HardMaxAttempts               = 20
	HardMaxReconciliationAttempts = 100
	HardMaxLease                  = 24 * time.Hour
	HardMaxRetryDelay             = 7 * 24 * time.Hour
)

type Config struct {
	// ClaimKinds limits execution and lease maintenance to selected consumers.
	// Empty preserves the existing general-purpose repository behavior.
	ClaimKinds                []string
	MaxPayloadBytes           int
	MaxAttempts               int
	MaxReconciliationAttempts int
	MaxLease                  time.Duration
	MaxRetryDelay             time.Duration
}

type Store struct {
	db     *sql.DB
	config Config
}

func New(db *sql.DB, cfg Config) (*Store, error) {
	if db == nil || !validConfig(cfg) {
		return nil, ErrInvalidConfig
	}
	cfg.ClaimKinds = append([]string(nil), cfg.ClaimKinds...)
	return &Store{db: db, config: cfg}, nil
}

func validConfig(cfg Config) bool {
	if cfg.MaxPayloadBytes <= 0 || cfg.MaxPayloadBytes > HardMaxPayloadBytes || cfg.MaxAttempts <= 0 || cfg.MaxAttempts > HardMaxAttempts || cfg.MaxReconciliationAttempts <= 0 || cfg.MaxReconciliationAttempts > HardMaxReconciliationAttempts || cfg.MaxLease <= 0 || cfg.MaxLease > HardMaxLease || cfg.MaxRetryDelay < 0 || cfg.MaxRetryDelay > HardMaxRetryDelay {
		return false
	}
	if len(cfg.ClaimKinds) > 16 {
		return false
	}
	seen := make(map[string]bool)
	for _, kind := range cfg.ClaimKinds {
		if kind == "" || len(kind) > 128 || strings.TrimSpace(kind) != kind || strings.IndexFunc(kind, unicode.IsControl) >= 0 || seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}

func (s *Store) Enqueue(ctx context.Context, in jobs.Intent) (jobs.Job, error) {
	if s == nil || s.db == nil || ctx == nil {
		return jobs.Job{}, ErrInvalidConfig
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	job, err := s.EnqueueTx(ctx, tx, in)
	if err != nil {
		_ = tx.Rollback()
		return jobs.Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	return job, nil
}

// EnqueueTx adds durable intent to a caller-owned domain transaction.
func (s *Store) EnqueueTx(ctx context.Context, tx *sql.Tx, in jobs.Intent) (jobs.Job, error) {
	if s == nil || ctx == nil || tx == nil {
		return jobs.Job{}, ErrInvalidConfig
	}
	if err := jobs.ValidateIntent(in, s.config.MaxPayloadBytes); err != nil {
		return jobs.Job{}, err
	}
	if !json.Valid(in.Payload) {
		return jobs.Job{}, jobs.ErrInvalidIntent
	}
	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	if !in.Deadline.After(databaseNow) {
		return jobs.Job{}, jobs.ErrInvalidIntent
	}
	id, err := uuid.NewV7()
	if err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	hash := jobs.RequestHash(in)
	deadline := in.Deadline.UTC()
	var insertedID uuid.UUID
	err = tx.QueryRowContext(ctx, `INSERT INTO amos_jobs
		(id, installation_id, application_id, idempotency_key, request_hash, kind, payload, external_effect, status, max_attempts, max_reconciliation_attempts, available_at, next_reconciliation_at, deadline_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,'queued',$9,$10,clock_timestamp(),clock_timestamp(),$11)
		ON CONFLICT (installation_id, application_id, idempotency_key) DO NOTHING RETURNING id`,
		id, in.InstallationID, in.ApplicationID, in.Key, hash[:], in.Kind, string(in.Payload), in.ExternalEffect,
		s.config.MaxAttempts, s.config.MaxReconciliationAttempts, deadline).Scan(&insertedID)
	if err == nil {
		return s.getTx(ctx, tx, insertedID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, ErrUnavailable
	}
	existing, getErr := s.findIdempotentTx(ctx, tx, in)
	if getErr != nil {
		return jobs.Job{}, getErr
	}
	if existing.RequestHash != hash {
		return jobs.Job{}, jobs.ErrIdempotencyConflict
	}
	return existing, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (jobs.Job, error) {
	if s == nil || s.db == nil || ctx == nil {
		return jobs.Job{}, ErrInvalidConfig
	}
	return scanJob(s.db.QueryRowContext(ctx, selectJob+` WHERE id=$1`, id))
}

func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration) (jobs.Job, bool, error) {
	if s == nil || s.db == nil || ctx == nil || owner == "" || len(owner) > 128 || owner != strings.TrimSpace(owner) || lease <= 0 || lease > s.config.MaxLease {
		return jobs.Job{}, false, ErrInvalidConfig
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, false, ErrUnavailable
	}
	defer func() { _ = tx.Rollback() }() // Commit consumes the transaction; failures already return unavailable.
	_, err = tx.ExecContext(ctx, `UPDATE amos_jobs SET status='dead', lease_owner=NULL, lease_action=NULL, lease_until=NULL, updated_at=clock_timestamp()
		WHERE status='queued' AND deadline_at <= clock_timestamp() AND (COALESCE(cardinality($1::text[]),0)=0 OR kind=ANY($1::text[]))`, s.config.ClaimKinds)
	if err != nil {
		return jobs.Job{}, false, ErrUnavailable
	}
	_, err = tx.ExecContext(ctx, `UPDATE amos_jobs SET
		status=CASE WHEN external_effect THEN 'unknown' WHEN attempt_count >= max_attempts THEN 'dead' ELSE 'queued' END,
		manual_review=CASE WHEN external_effect AND reconciliation_attempt_count >= max_reconciliation_attempts THEN true ELSE manual_review END,
		lease_owner=NULL, lease_action=NULL, lease_until=NULL, next_reconciliation_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE lease_owner IS NOT NULL AND lease_until <= clock_timestamp() AND (COALESCE(cardinality($1::text[]),0)=0 OR kind=ANY($1::text[]))`, s.config.ClaimKinds)
	if err != nil {
		return jobs.Job{}, false, ErrUnavailable
	}
	var result jobs.Job
	result, err = scanClaim(tx.QueryRowContext(ctx, `WITH candidate AS (
		SELECT id, status FROM amos_jobs
		WHERE ((status='queued' AND available_at <= clock_timestamp() AND deadline_at > clock_timestamp())
		 OR (status='unknown' AND manual_review=false AND lease_owner IS NULL AND next_reconciliation_at <= clock_timestamp() AND reconciliation_attempt_count < max_reconciliation_attempts))
		AND (COALESCE(cardinality($3::text[]),0)=0 OR kind=ANY($3::text[]))
		ORDER BY CASE WHEN status='queued' THEN available_at ELSE next_reconciliation_at END, created_at, id
		FOR UPDATE SKIP LOCKED LIMIT 1
	)
	UPDATE amos_jobs j SET
		status=CASE WHEN c.status='queued' THEN 'leased' ELSE 'unknown' END,
		attempt_count=j.attempt_count + CASE WHEN c.status='queued' THEN 1 ELSE 0 END,
		reconciliation_attempt_count=j.reconciliation_attempt_count + CASE WHEN c.status='unknown' THEN 1 ELSE 0 END,
		lease_owner=$1, lease_action=CASE WHEN c.status='queued' THEN 'execute' ELSE 'reconcile' END,
		fence_token=j.fence_token+1,
		lease_until=clock_timestamp()+($2 * interval '1 microsecond'), updated_at=clock_timestamp()
	FROM candidate c WHERE j.id=c.id
	RETURNING `+jobReturningColumns, owner, lease.Microseconds(), s.config.ClaimKinds))
	if errors.Is(err, jobs.ErrNotFound) {
		if err := tx.Commit(); err != nil {
			return jobs.Job{}, false, ErrUnavailable
		}
		return jobs.Job{}, false, nil
	}
	if err != nil {
		return jobs.Job{}, false, ErrUnavailable
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, false, ErrUnavailable
	}
	return result, true, nil
}

func (s *Store) Resolve(ctx context.Context, job jobs.Job, owner string, resolution jobs.Resolution) error {
	if s == nil || s.db == nil || ctx == nil || job.ID == uuid.Nil || owner == "" || job.FenceToken <= 0 || jobs.ValidateResolution(resolution) != nil || resolution.RetryAfter > s.config.MaxRetryDelay || resolution.Kind == jobs.ResolutionUnknown && !job.ExternalEffect {
		return jobs.ErrInvalidResolution
	}
	var status jobs.State
	var next time.Duration
	var manual bool
	switch resolution.Kind {
	case jobs.ResolutionSucceeded:
		status = jobs.StateSucceeded
	case jobs.ResolutionTerminal:
		status = jobs.StateDead
	case jobs.ResolutionRetrySafe:
		if job.Action != jobs.ActionExecute {
			return jobs.ErrInvalidResolution
		}
		if job.Attempt >= job.MaxAttempts {
			status = jobs.StateDead
		} else {
			status = jobs.StateQueued
			next = resolution.RetryAfter
		}
	case jobs.ResolutionNoEffect:
		if job.Action != jobs.ActionReconcile {
			return jobs.ErrInvalidResolution
		}
		if job.Attempt >= job.MaxAttempts {
			status = jobs.StateDead
		} else {
			status = jobs.StateQueued
			next = resolution.RetryAfter
		}
	case jobs.ResolutionUnknown:
		if job.Action != jobs.ActionExecute && job.Action != jobs.ActionReconcile {
			return jobs.ErrInvalidResolution
		}
		status = jobs.StateUnknown
		next = resolution.RetryAfter
		manual = job.Action == jobs.ActionReconcile && job.ReconciliationAttempt >= job.MaxReconciliationAttempts
	}
	res, err := s.db.ExecContext(ctx, `UPDATE amos_jobs SET status=$1, available_at=clock_timestamp()+($2 * interval '1 microsecond'),
		next_reconciliation_at=clock_timestamp()+($2 * interval '1 microsecond'), manual_review=$3,
		lease_owner=NULL, lease_action=NULL, lease_until=NULL, updated_at=clock_timestamp()
		WHERE id=$4 AND fence_token=$5 AND lease_owner=$6 AND lease_action=$7 AND lease_until > clock_timestamp() AND
		((lease_action='execute' AND status='leased') OR (lease_action='reconcile' AND status='unknown'))`,
		status, next.Microseconds(), manual, job.ID, job.FenceToken, owner, job.Action)
	if err != nil {
		return ErrUnavailable
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if n == 0 {
		return s.classifyLeaseFailure(ctx, job.ID)
	}
	return nil
}

func (s *Store) classifyLeaseFailure(ctx context.Context, id uuid.UUID) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM amos_jobs WHERE id=$1)`, id).Scan(&exists); err != nil {
		return ErrUnavailable
	}
	if !exists {
		return jobs.ErrNotFound
	}
	return jobs.ErrLeaseLost
}

const jobColumns = `id, installation_id, application_id, idempotency_key, request_hash, kind, payload::text,
	external_effect, status, attempt_count, max_attempts, reconciliation_attempt_count, max_reconciliation_attempts,
	deadline_at, COALESCE(lease_owner,''), COALESCE(lease_action,''), fence_token, lease_until, manual_review, created_at`
const jobReturningColumns = `j.id, j.installation_id, j.application_id, j.idempotency_key, j.request_hash, j.kind, j.payload::text,
	j.external_effect, j.status, j.attempt_count, j.max_attempts, j.reconciliation_attempt_count, j.max_reconciliation_attempts,
	j.deadline_at, COALESCE(j.lease_owner,''), COALESCE(j.lease_action,''), j.fence_token, j.lease_until, j.manual_review, j.created_at`
const selectJob = `SELECT ` + jobColumns + ` FROM amos_jobs`

type rowScanner interface{ Scan(...any) error }

func scanJob(row rowScanner) (jobs.Job, error) {
	var j jobs.Job
	var hash []byte
	var payload string
	var lease sql.NullTime
	var state string
	var action string
	err := row.Scan(&j.ID, &j.InstallationID, &j.ApplicationID, &j.Key, &hash, &j.Kind, &payload, &j.ExternalEffect, &state, &j.Attempt, &j.MaxAttempts, &j.ReconciliationAttempt, &j.MaxReconciliationAttempts, &j.Deadline, &j.LeaseOwner, &action, &j.FenceToken, &lease, &j.ManualReview, &j.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, jobs.ErrNotFound
	}
	if err != nil {
		return jobs.Job{}, ErrUnavailable
	}
	copy(j.RequestHash[:], hash)
	j.Payload = []byte(payload)
	j.State = jobs.State(state)
	j.Action = jobs.Action(action)
	if lease.Valid {
		j.LeaseUntil = lease.Time
	}
	return j, nil
}
func scanClaim(row rowScanner) (jobs.Job, error) { return scanJob(row) }

func (s *Store) getTx(ctx context.Context, tx *sql.Tx, id uuid.UUID) (jobs.Job, error) {
	return scanJob(tx.QueryRowContext(ctx, selectJob+` WHERE id=$1`, id))
}
func (s *Store) findIdempotentTx(ctx context.Context, tx *sql.Tx, in jobs.Intent) (jobs.Job, error) {
	return scanJob(tx.QueryRowContext(ctx, selectJob+` WHERE installation_id=$1 AND application_id=$2 AND idempotency_key=$3`, in.InstallationID, in.ApplicationID, in.Key))
}
