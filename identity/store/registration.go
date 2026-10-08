package store

import (
	"context"
	"database/sql"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

// PendingRegistration proves that this store created the pending person in the
// caller's still-open transaction. It grants no authenticated authority.
// Its zero value and a capability from another transaction are unusable.
type PendingRegistration struct {
	tx                                      *sql.Tx
	attempt                                 *aw.Attempt
	binding                                 aw.Binding
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
	var binding aw.Binding
	if s.attempt != nil {
		var err error
		binding, err = s.attempt.Binding()
		if err != nil {
			return PendingRegistration{}, s.failed(ErrPersistence)
		}
	}
	return PendingRegistration{tx: s.tx, attempt: s.attempt, binding: binding, personID: input.PersonID, installationID: input.InstallationID, applicationID: input.ApplicationID}, nil
}

// InAttempt requires both original identity and a still-live acquired parent.
func (r PendingRegistration) InAttempt(a *aw.Attempt) bool {
	return r.attempt != nil && r.binding.Matches(a) && a.CheckRows(aw.P, []aw.Row{row(aw.Persons, r.personID, aw.ReservedInsert)}) == nil
}
