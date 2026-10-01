package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
)

// FindCurrentPassword locks the active account and password credential for a
// caller transaction after a request-bound principal has supplied its epoch.
func (s *Store) FindCurrentPassword(ctx context.Context, scope SessionScope, person uuid.UUID, epoch int64) (string, error) {
	if s == nil || s.tx == nil || ctx == nil || !validID(person) || !validID(scope.InstallationID) || !validID(scope.ApplicationID) || !validID(scope.EnvironmentID) || epoch < 0 {
		return "", ErrInvalidInput
	}
	var encoded string
	err := s.tx.QueryRowContext(ctx, `SELECT c.verifier_hash FROM identity_persons p JOIN identity_credentials c ON c.person_id=p.id AND c.method='email_password' AND c.revoked_at IS NULL WHERE p.id=$1 AND p.installation_id=$2 AND p.application_id=$3 AND p.security_epoch=$4 AND p.state='active' AND EXISTS(SELECT 1 FROM identity_emails e WHERE e.person_id=p.id AND e.installation_id=p.installation_id AND e.application_id=p.application_id AND e.verified_at IS NOT NULL) FOR UPDATE OF p,c`, person, scope.InstallationID, scope.ApplicationID, epoch).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSessionUnavailable
	}
	if err != nil {
		return "", ErrPersistence
	}
	return encoded, nil
}
