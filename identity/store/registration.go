package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// PendingRegistration proves that this store created the pending person in the
// caller's still-open transaction. It grants no authenticated authority.
// Its zero value and a capability from another transaction are unusable.
type PendingRegistration struct {
	tx                                      *sql.Tx
	personID, installationID, applicationID uuid.UUID
}

func (r PendingRegistration) PersonID() uuid.UUID           { return r.personID }
func (r PendingRegistration) InstallationID() uuid.UUID     { return r.installationID }
func (r PendingRegistration) ApplicationID() uuid.UUID      { return r.applicationID }
func (r PendingRegistration) InTransaction(tx *sql.Tx) bool { return r.tx != nil && r.tx == tx }

// CreatePendingRegistration inserts the account and returns a transaction-bound
// creation proof for atomic personal workspace provisioning. The caller must
// roll back the transaction if any later participant fails.
func (s *Store) CreatePendingRegistration(ctx context.Context, input PendingAccount) (PendingRegistration, error) {
	if err := s.CreatePendingAccount(ctx, input); err != nil {
		return PendingRegistration{}, err
	}
	return PendingRegistration{tx: s.tx, personID: input.PersonID, installationID: input.InstallationID, applicationID: input.ApplicationID}, nil
}
