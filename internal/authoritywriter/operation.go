package authoritywriter

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// OperationPlan selects an ordering-only, non-issuing native operation profile.
// Neither these IDs nor a successfully constructed plan establish authority.
type OperationPlan struct {
	InvocationID uuid.UUID
	ActorID      uuid.UUID
	SessionID    uuid.UUID
	WorkspaceID  uuid.UUID
}

// NewOperationPlan copies the exact native identity/workspace lock inventory.
// Billing and domain locking remain obligations of the reviewed native adapters.
func NewOperationPlan(realm Realm, rows []Row, operation OperationPlan) (Plan, error) {
	for _, id := range []uuid.UUID{operation.InvocationID, operation.ActorID, operation.SessionID, operation.WorkspaceID} {
		if !validID(id) {
			return Plan{}, ErrUnrooted
		}
	}
	p, err := NewPlan(realm, rows, nil)
	if err != nil {
		return Plan{}, err
	}
	if len(p.data.rows) < 3 || len(p.data.rows) > 4 {
		return Plan{}, ErrUnrooted
	}
	required := map[Table]uuid.UUID{Persons: operation.ActorID, Sessions: operation.SessionID, Workspaces: operation.WorkspaceID}
	seen := make(map[Table]bool, 4)
	for _, row := range p.data.rows {
		if row.Access != ExistingUpdate || row.ID == operation.InvocationID || seen[row.Table] {
			return Plan{}, ErrUnrooted
		}
		want, requiredTable := required[row.Table]
		if (!requiredTable && row.Table != Memberships) || (requiredTable && row.ID != want) {
			return Plan{}, ErrUnrooted
		}
		seen[row.Table] = true
	}
	for table := range required {
		if !seen[table] {
			return Plan{}, ErrUnrooted
		}
	}
	p.data.operation = &operation
	return p, nil
}

// OperationStep names a finite native substep inside the existing D phase.
// Completing a step records ordering, not successful authorization or SQL truth.
type OperationStep uint8

const (
	OperationAuthority OperationStep = iota + 1
	OperationResources
	OperationClaim
	OperationCallback
	OperationResult
	OperationAudit
	OperationReplay
	OperationDeniedAudit
	OperationUnavailableAudit
)

func operationNext(previous, next OperationStep) bool {
	switch previous {
	case 0:
		return next == OperationAuthority
	case OperationAuthority:
		return next == OperationResources || next == OperationDeniedAudit || next == OperationUnavailableAudit
	case OperationResources:
		return next == OperationClaim || next == OperationDeniedAudit || next == OperationUnavailableAudit
	case OperationClaim:
		return next == OperationCallback || next == OperationReplay
	case OperationCallback:
		return next == OperationResult
	case OperationResult:
		return next == OperationAudit
	default:
		return false
	}
}
func (s *attemptState) operationTerminal() bool {
	if s.operationOpen != 0 {
		return false
	}
	switch s.operationCompleted {
	case OperationAudit, OperationReplay, OperationDeniedAudit, OperationUnavailableAudit:
		return true
	default:
		return false
	}
}
func (s *attemptState) operationOutcome(outcome Outcome) bool {
	if !s.operationTerminal() {
		return false
	}
	switch outcome {
	case Success:
		return s.operationCompleted == OperationAudit || s.operationCompleted == OperationReplay
	case OperationDeniedCommitted:
		return s.operationCompleted == OperationDeniedAudit
	case OperationUnavailableCommitted:
		return s.operationCompleted == OperationUnavailableAudit
	default:
		return false
	}
}

// OperationTx returns the original transaction only to trusted native adapters.
// Business callbacks must instead receive the existing guarded operation.DBTX.
// It cannot prove adapter lock order or inspect arbitrary SQL and is not a sandbox.
func (a *Attempt) OperationTx(ctx context.Context, step OperationStep) (*sql.Tx, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return nil, err
	}
	if err = s.checkContext(ctx); err != nil {
		return nil, err
	}
	if s.plan == nil || s.plan.operation == nil || (s.phase != W && s.phase != D) ||
		len(s.journal) != 0 || s.counter != uuid.Nil || len(s.plan.delivery) != 0 ||
		s.deliveryStep != 0 || s.deliveryRecorded != 0 || s.operationOpen != 0 ||
		!operationNext(s.operationCompleted, step) {
		return nil, s.poison(ErrPhase)
	}
	s.phase = D
	s.operationOpen = step
	return s.tx, nil
}

// CompleteOperationStep closes exactly the currently open native step. A failed
// SQL/adapter call must terminate the attempt, not call this method and continue.
func (a *Attempt) CompleteOperationStep(ctx context.Context, step OperationStep) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	if err = s.checkContext(ctx); err != nil {
		return err
	}
	if s.plan == nil || s.plan.operation == nil || s.phase != D || step == 0 || s.operationOpen != step {
		return s.poison(ErrPhase)
	}
	s.operationCompleted = step
	s.operationOpen = 0
	return nil
}
