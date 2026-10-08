package store

import (
	"context"
	"github.com/google/uuid"
	"time"
)

// SetSessionAssurance is a caller-transaction write for a newly verified
// session. The database also bounds assurance to fifteen minutes.
func (s *Store) SetSessionAssurance(ctx context.Context, id uuid.UUID, level string, expires time.Time) error {
	if s == nil || s.tx == nil || ctx == nil || !validID(id) || (level != "aal2" && level != "aal3") || expires.IsZero() {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, `UPDATE identity_sessions SET assurance_level=$2,assurance_expires_at=$3 WHERE id=$1 AND revoked_at IS NULL AND expires_at>transaction_timestamp()`, id, level, expires)
	if err != nil {
		return ErrPersistence
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrSessionUnavailable
	}
	return nil
}

// ActiveSessionAssurance must follow FindActiveSession in the same transaction;
// that lookup holds the session row lock and verifies account state and epoch.
// One fresh database instant governs session lifetime and both assurance values;
// equality at either expiry boundary is expired.
func (s *Store) ActiveSessionAssurance(ctx context.Context, id uuid.UUID) (string, time.Time, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(id) {
		return "", time.Time{}, ErrInvalidInput
	}
	var now time.Time
	if err := s.tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return "", time.Time{}, ErrPersistence
	}
	rows, err := s.tx.QueryContext(ctx, `SELECT
		CASE WHEN assurance_expires_at>$2 THEN assurance_level ELSE 'aal1' END,
		CASE WHEN assurance_expires_at>$2 THEN assurance_expires_at ELSE expires_at END
		FROM identity_sessions WHERE id=$1 AND revoked_at IS NULL
		AND idle_expires_at>$2 AND expires_at>$2`, id, now)
	if err != nil {
		return "", time.Time{}, ErrPersistence
	}
	if !rows.Next() {
		readErr, closeErr := rows.Err(), rows.Close()
		if readErr != nil || closeErr != nil {
			return "", time.Time{}, ErrPersistence
		}
		return "", time.Time{}, ErrSessionUnavailable
	}
	var level string
	var expires time.Time
	scanErr, closeErr := rows.Scan(&level, &expires), rows.Close()
	if scanErr != nil || closeErr != nil {
		return "", time.Time{}, ErrPersistence
	}
	return level, expires, nil
}
