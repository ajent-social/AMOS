// Package repository stores continuity values in a caller-owned transaction.
// Scope selects data; it is never authority. The caller MUST roll back on every
// error and must not disclose provisional results until its commit succeeds.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ajent-social/amos/app/operation"
	"github.com/google/uuid"
)

var (
	ErrInvalid          = errors.New("invalid continuity input")
	ErrNotFound         = errors.New("continuity record not found")
	ErrConflict         = errors.New("continuity revision conflict")
	ErrDecisionDisabled = errors.New("human decision is disabled")
	ErrUnavailable      = errors.New("continuity storage unavailable")
)

type Scope struct{ InstallationID, ApplicationID, EnvironmentID, WorkspaceID uuid.UUID }
type Repository struct {
	tx    operation.DBTX
	scope Scope
}
type Property struct {
	ID                                               string
	Name, Area, OwnerLabel, OccupantLabel, Occupancy string
	InspectionDate                                   string
}
type Source struct{ ID, Kind, Title, Body, SHA256 string }
type SourceSummary struct{ ID, Kind, Title, SHA256 string }
type Activity struct {
	ID, ActorID, Action, ResourceID string
	Revision                        int64
	At                              time.Time
}

// New retains the caller's transaction, never its pool or credentials. The caller
// uses READ COMMITTED and serializes calls; this repository is not concurrency-safe.
func New(tx *sql.Tx, scope Scope) (*Repository, error) {
	if tx == nil || !validScope(scope) {
		return nil, ErrInvalid
	}
	return &Repository{tx: transactionDB{tx}, scope: scope}, nil
}
func validScope(s Scope) bool {
	for _, id := range []uuid.UUID{s.InstallationID, s.ApplicationID, s.EnvironmentID, s.WorkspaceID} {
		if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return false
		}
	}
	return true
}
func (r *Repository) ready(ctx context.Context) error {
	if r == nil || r.tx == nil || ctx == nil || !validScope(r.scope) {
		return ErrInvalid
	}
	return nil
}
func (r *Repository) args(rest ...any) []any {
	return append([]any{r.scope.InstallationID, r.scope.ApplicationID, r.scope.EnvironmentID, r.scope.WorkspaceID}, rest...)
}

const scoped = `installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4`

func readError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return ErrUnavailable
}
