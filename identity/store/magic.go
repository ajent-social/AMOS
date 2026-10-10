package store

import (
	"context"
)

// CreateMagicChallenge binds a random browser-cookie digest to an email proof.
// The caller validates current person/scope/policy and commits delivery material
// and outbox in this same transaction. Raw browser/email secrets are excluded.
func (s *Store) createMagicChallenge(ctx context.Context, challenge Challenge, browserDigest []byte) error {
	if challenge.Purpose != ChallengeEmailMagicLink || len(browserDigest) != 32 {
		return ErrInvalidInput
	}
	if err := s.createChallenge(ctx, challenge); err != nil {
		return err
	}
	result, err := s.tx.ExecContext(ctx, "UPDATE identity_challenges SET browser_binding_digest=$2 WHERE id=$1 AND purpose='email_magic_link' AND consumed_at IS NULL", challenge.ID, browserDigest)
	if err != nil {
		return ErrPersistence
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if rows != 1 {
		return ErrChallengeUnavailable
	}
	return nil
}

// RevokeSessionScoped rotates only a session in the configured deployment.
// Untrusted browser cookies cannot revoke another application's session.
func (s *Store) revokeSessionScoped(ctx context.Context, digest []byte, scope SessionScope) error {
	if s == nil || s.tx == nil || ctx == nil || len(digest) != 32 || !validID(scope.InstallationID) || !validID(scope.ApplicationID) || !validID(scope.EnvironmentID) {
		return ErrInvalidInput
	}
	result, err := s.tx.ExecContext(ctx, "UPDATE identity_sessions SET revoked_at=transaction_timestamp() WHERE token_digest=$1 AND installation_id=$2 AND application_id=$3 AND environment_id=$4 AND revoked_at IS NULL", digest, scope.InstallationID, scope.ApplicationID, scope.EnvironmentID)
	if err != nil {
		return ErrPersistence
	}
	n, err := result.RowsAffected()
	if err != nil {
		return ErrPersistence
	}
	if n != 1 {
		return ErrSessionUnavailable
	}
	return nil
}
