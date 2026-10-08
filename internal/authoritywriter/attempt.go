package authoritywriter

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Attempt struct{ state *attemptState }
type attemptState struct {
	mu               sync.Mutex
	root             *rootState
	token            *identityToken
	tx               *sql.Tx
	deadline         time.Time
	startedAt        time.Time
	databaseDeadline time.Time
	done             <-chan struct{}
	live, finished   bool
	failed           error
	plan             *planData
	phase            Phase
	outcome          Outcome
	sample           time.Time
	deliveryStep     DeliveryStep
	deliveryRecorded DeliveryStep
	counter          uuid.UUID
	journal          []mutationRecord
}
type mutationRecord struct {
	kind     Mutation
	rows     []Row
	delivery DeliveryStep
}

// lockLive returns a locked state. Callers must unlock it even on a live-state
// error. The immutable binding remains usable after closure; SQL does not.
func (a *Attempt) lockLive() (*attemptState, error) {
	if a == nil || a.state == nil {
		return nil, ErrUnrooted
	}
	s := a.state
	s.mu.Lock()
	if !s.live || s.tx == nil {
		return s, ErrClosed
	}
	if s.finished {
		return s, s.poison(ErrClosed)
	}
	if s.failed != nil {
		return s, s.failed
	}
	if !active(s.root) {
		return s, s.poison(ErrUnrooted)
	}
	select {
	case <-s.done:
		return s, s.poison(ErrUnavailable)
	default:
	}
	if !time.Now().Before(s.deadline) {
		return s, s.poison(ErrUnavailable)
	}
	return s, nil
}
func unlock(s *attemptState) {
	if s != nil {
		s.mu.Unlock()
	}
}
func (s *attemptState) poison(err error) error {
	if s.failed == nil {
		s.failed = err
	}
	return s.failed
}
func (s *attemptState) checkContext(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || ctx.Value(requestKey{}) != s.token {
		return s.poison(ErrUnrooted)
	}
	d, ok := ctx.Deadline()
	if !ok || d.IsZero() || d.After(s.deadline) || !time.Now().Before(d) {
		return s.poison(ErrUnavailable)
	}
	return nil
}
func (a *Attempt) DiscoveryTx(ctx context.Context) (*sql.Tx, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return nil, err
	}
	if s.plan != nil || s.phase != 0 {
		return nil, s.poison(ErrPhase)
	}
	if err = s.checkContext(ctx); err != nil {
		return nil, err
	}
	return s.tx, nil
}
func (a *Attempt) SealPlan(plan Plan) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	if plan.data == nil || s.plan != nil || s.phase != 0 {
		return s.poison(ErrPhase)
	}
	s.plan = plan.data
	return nil
}
func (a *Attempt) Binding() (Binding, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return Binding{}, err
	}
	if s.plan == nil {
		return Binding{}, s.poison(ErrPhase)
	}
	return Binding{state: s.token}, nil
}
func (a *Attempt) Realm() (Realm, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return Realm{}, err
	}
	if s.plan == nil {
		return Realm{}, s.poison(ErrPhase)
	}
	return s.plan.realm, nil
}

// StartedAt returns the one database B sampled after G. It cannot refresh the
// original root deadline or provide identity evidence.
func (a *Attempt) StartedAt() (time.Time, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return time.Time{}, err
	}
	if s.startedAt.IsZero() {
		return time.Time{}, s.poison(ErrUnrooted)
	}
	return s.startedAt, nil
}

// CheckRows lets native evidence factories inspect ordering without SQL access.
// The original request cancellation and deadline remain mandatory even though
// this method accepts no replacement context.
func (a *Attempt) CheckRows(phase Phase, rows []Row) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	return s.checkRows(phase, rows)
}

func (a *Attempt) Acquire(ctx context.Context, phase Phase) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	if err = s.checkContext(ctx); err != nil {
		return err
	}
	if s.plan == nil || phase < P || phase > W || phase != s.phase+1 {
		return s.poison(ErrPhase)
	}
	for _, row := range s.plan.rows {
		if tablePhase(row.Table) != phase {
			continue
		}
		if err := lockRow(ctx, s.tx, s.plan.realm, row); err != nil {
			return s.poison(err)
		}
	}
	s.phase = phase
	return nil
}
func (a *Attempt) ParticipantTx(ctx context.Context, phase Phase, rows []Row) (*sql.Tx, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return nil, err
	}
	if err = s.checkContext(ctx); err != nil {
		return nil, err
	}
	if err = s.checkRows(phase, rows); err != nil {
		return nil, err
	}
	return s.tx, nil
}
func (s *attemptState) checkRows(phase Phase, rows []Row) error {
	if s.plan == nil || phase < P || phase > W || s.phase < phase || s.phase >= F || len(rows) == 0 {
		return s.poison(ErrPhase)
	}
	seen := make(map[Row]bool, len(rows))
	for _, row := range rows {
		if tablePhase(row.Table) > phase || !s.plan.contains(row) || seen[row] {
			return s.poison(ErrUnrooted)
		}
		seen[row] = true
	}
	return nil
}

type DeliveryStep uint8

const (
	MaterialInsert DeliveryStep = iota + 1
	JobEnqueue
)

func (a *Attempt) DeliveryTx(ctx context.Context, delivery Delivery, step DeliveryStep) (*sql.Tx, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return nil, err
	}
	if err = s.checkContext(ctx); err != nil {
		return nil, err
	}
	if s.plan == nil || len(s.plan.delivery) != 1 || s.plan.delivery[0] != delivery || (s.phase != W && s.phase != D) || step != s.deliveryStep+1 || step > JobEnqueue || s.deliveryRecorded != s.deliveryStep || s.counter != uuid.Nil {
		return nil, s.poison(ErrPhase)
	}
	s.phase = D
	s.deliveryStep = step
	return s.tx, nil
}

type Mutation uint8

const (
	RegistrationWrite Mutation = iota + 1
	CredentialWrite
	ContactWrite
	ChallengeWrite
	FactorSuccessWrite
	CounterWrite
	FlowWrite
	BindingWrite
	SessionWrite
	WorkspaceWrite
	DeliveryWrite
)

func mutationAllows(kind Mutation, row Row) bool {
	switch kind {
	case RegistrationWrite:
		return row.Table == Persons || row.Table == Emails || row.Table == Credentials
	case CredentialWrite:
		return row.Table == Persons || row.Table == Credentials
	case ContactWrite:
		return row.Table == Persons || row.Table == Emails
	case ChallengeWrite:
		return row.Table == Challenges
	case FactorSuccessWrite, CounterWrite:
		return row.Table == Factors
	case FlowWrite:
		return row.Table == Flows
	case BindingWrite:
		return row.Table == Bindings
	case SessionWrite:
		return row.Table == Sessions
	case WorkspaceWrite:
		return row.Table == Workspaces || row.Table == Memberships
	default:
		return false
	}
}
func (a *Attempt) RestrictCounter(factorID uuid.UUID) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	if s.plan == nil || s.phase != W || s.counter != uuid.Nil || len(s.journal) != 0 || !s.plan.contains(Row{Table: Factors, ID: factorID, Access: ExistingUpdate}) || len(s.plan.delivery) != 0 {
		return s.poison(ErrPhase)
	}
	for _, row := range s.plan.rows {
		if row.Table == Factors && row.ID != factorID {
			return s.poison(ErrPhase)
		}
	}
	s.counter = factorID
	return nil
}
func (a *Attempt) RecordMutation(kind Mutation, rows []Row) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	if kind < RegistrationWrite || kind > DeliveryWrite {
		return s.poison(ErrPhase)
	}
	if s.counter != uuid.Nil {
		if kind != CounterWrite || len(s.journal) != 0 || len(rows) != 1 || rows[0] != (Row{Table: Factors, ID: s.counter, Access: ExistingUpdate}) {
			return s.poison(ErrPhase)
		}
	} else if kind == CounterWrite {
		return s.poison(ErrPhase)
	}
	if kind == DeliveryWrite {
		if len(rows) != 0 || s.phase != D || s.deliveryStep == 0 || s.deliveryRecorded == s.deliveryStep {
			return s.poison(ErrPhase)
		}
		s.deliveryRecorded = s.deliveryStep
	} else {
		minimum := P
		for _, row := range rows {
			if !mutationAllows(kind, row) || row.Access == ExistingShare {
				return s.poison(ErrPhase)
			}
			if p := tablePhase(row.Table); p > minimum {
				minimum = p
			}
		}
		if err = s.checkRows(minimum, rows); err != nil {
			return err
		}
		for _, old := range s.journal {
			if old.kind != kind {
				continue
			}
			for _, before := range old.rows {
				for _, row := range rows {
					if before == row {
						return s.poison(ErrPhase)
					}
				}
			}
		}
	}
	s.journal = append(s.journal, mutationRecord{kind: kind, rows: append([]Row(nil), rows...), delivery: s.deliveryStep})
	return nil
}
func (a *Attempt) DrainAndSample(ctx context.Context) (time.Time, error) {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return time.Time{}, err
	}
	if err = s.checkContext(ctx); err != nil {
		return time.Time{}, err
	}
	if s.plan == nil || (s.phase != W && s.phase != D) || (len(s.plan.delivery) != 0 && (s.deliveryStep != JobEnqueue || s.deliveryRecorded != JobEnqueue)) {
		return time.Time{}, s.poison(ErrPhase)
	}
	if _, err = s.tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return time.Time{}, s.poison(ErrUnavailable)
	}
	var now time.Time
	if err = s.tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&now); err != nil || now.IsZero() {
		return time.Time{}, s.poison(ErrUnavailable)
	}
	if !s.validFinalTime(now) {
		return time.Time{}, s.poison(ErrUnavailable)
	}
	s.phase = F
	s.sample = now
	return now, nil
}

func (s *attemptState) validFinalTime(now time.Time) bool {
	return !now.IsZero() && !s.startedAt.IsZero() && !s.databaseDeadline.IsZero() &&
		!now.Before(s.startedAt) && now.Before(s.databaseDeadline)
}

func (a *Attempt) IsFinalSample(t time.Time) bool {
	s, err := a.lockLive()
	defer unlock(s)
	return err == nil && s.phase == F && !t.IsZero() && t.Equal(s.sample)
}
func (a *Attempt) Finish(outcome Outcome) error {
	s, err := a.lockLive()
	defer unlock(s)
	if err != nil {
		return err
	}
	switch outcome {
	case Success:
		if s.phase != F || s.sample.IsZero() || s.counter != uuid.Nil {
			return s.poison(ErrPhase)
		}
	case CounterOnlyDenied:
		if s.phase != F || s.sample.IsZero() || s.counter == uuid.Nil || len(s.journal) != 1 || s.journal[0].kind != CounterWrite {
			return s.poison(ErrPhase)
		}
	case DeniedRollback, UnavailableRollback:
	default:
		return s.poison(ErrPhase)
	}
	s.finished = true
	s.outcome = outcome
	return nil
}
