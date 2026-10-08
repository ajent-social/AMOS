package authoritywriter

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ajent-social/amos/storage"
)

const rootBudget = 5 * time.Second

type Root struct{ state *rootState }
type rootState struct{ runtime *storage.RuntimeDB }

var profile struct {
	sync.Mutex
	legacy bool
	root   *rootState
}

// New retains precisely the supplied runtime. It neither opens nor owns its
// shutdown; the private composition owner closes it after all requests drain.
func New(runtime *storage.RuntimeDB) (*Root, error) {
	if runtime == nil {
		return nil, ErrUnrooted
	}
	return &Root{state: &rootState{runtime: runtime}}, nil
}
func ActivateW1(root *Root) error {
	profile.Lock()
	defer profile.Unlock()
	if root == nil || root.state == nil || root.state.runtime == nil || profile.legacy || profile.root != nil {
		return ErrUnrooted
	}
	profile.root = root.state
	return nil
}
func SelectLegacy() error {
	profile.Lock()
	defer profile.Unlock()
	if profile.root != nil {
		return ErrUnrooted
	}
	profile.legacy = true
	return nil
}
func active(r *rootState) bool {
	profile.Lock()
	defer profile.Unlock()
	return r != nil && r.runtime != nil && !profile.legacy && profile.root == r
}
func bounded(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, nil, ErrUnavailable
	}
	now := time.Now()
	end := now.Add(rootBudget)
	if original, ok := ctx.Deadline(); ok {
		if original.IsZero() || !original.After(now) {
			return nil, nil, ErrUnavailable
		}
		if original.Before(end) {
			end = original
		}
	}
	b, c := context.WithDeadline(ctx, end)
	return b, c, nil
}

func checkMode(ctx context.Context, tx *sql.Tx) error {
	var isolation, readOnly string
	if err := tx.QueryRowContext(ctx, `SELECT pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil || isolation != "read committed" || readOnly != "off" {
		return ErrUnavailable
	}
	return nil
}

// Read has no writer admission. Raw SQL is restricted to reviewed native reads;
// this API is trusted plumbing, not a database-enforced read-only sandbox.
func (r *Root) Read(ctx context.Context, body func(context.Context, *sql.Tx) error) (result error) {
	if r == nil || !active(r.state) || body == nil {
		return ErrUnrooted
	}
	ctx, cancel, err := bounded(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() {
		if recover() != nil {
			result = ErrUnavailable
		}
	}()
	err = r.state.runtime.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: false}, func(tx *sql.Tx) error {
		if err := checkMode(ctx, tx); err != nil {
			return err
		}
		if err := body(ctx, tx); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	return readCompletion(ctx, err)
}

// A successful driver Commit may race request cancellation. This terminal
// publication check cannot claim that an already successful commit rolled back.
func readCompletion(ctx context.Context, err error) error {
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return ErrDenied
		}
		return ErrUnavailable
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}

type identityToken struct{ nonzero byte }
type requestKey struct{}
type Binding struct{ state *identityToken }
type Outcome uint8

const (
	Success Outcome = iota + 1
	DeniedRollback
	CounterOnlyDenied
	UnavailableRollback
)

type Completion struct{ state *completionState }
type completionState struct {
	binding *identityToken
	outcome Outcome
	used    atomic.Bool
}
type Release struct{ data *releaseData }
type releaseData struct {
	binding *identityToken
	outcome Outcome
}

// Run is the sole transaction owner. A callback failure, panic, missing Finish,
// cancellation or failed/unknown commit yields no Completion and no retry.
func (r *Root) Run(ctx context.Context, body func(context.Context, *Attempt) Outcome) (completion Completion, result error) {
	if r == nil || !active(r.state) || body == nil {
		return Completion{}, ErrUnrooted
	}
	ctx, cancel, err := bounded(ctx)
	if err != nil {
		return Completion{}, err
	}
	defer cancel()
	token := &identityToken{nonzero: 1}
	ctx = context.WithValue(ctx, requestKey{}, token)
	deadline, _ := ctx.Deadline()
	a := &Attempt{state: &attemptState{root: r.state, token: token, deadline: deadline, done: ctx.Done(), live: true}}
	defer func() {
		a.state.mu.Lock()
		a.state.live = false
		a.state.tx = nil
		a.state.mu.Unlock()
		if recover() != nil {
			completion = Completion{}
			result = ErrUnavailable
		}
	}()
	err = r.state.runtime.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: false}, func(tx *sql.Tx) error {
		if err := checkMode(ctx, tx); err != nil {
			return err
		}
		var gate int
		if err := tx.QueryRowContext(ctx, `SELECT id FROM public.identity_writer_gate WHERE id = 1 FOR UPDATE`).Scan(&gate); err != nil || gate != 1 {
			return ErrUnavailable
		}
		var started time.Time
		if err := tx.QueryRowContext(ctx, `SELECT pg_catalog.clock_timestamp()`).Scan(&started); err != nil || started.IsZero() {
			return ErrUnavailable
		}
		// Freeze the remaining original budget immediately after B returns.
		// Neither phase entry nor later DB samples can refresh this bound.
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > rootBudget {
			return ErrUnavailable
		}
		a.state.mu.Lock()
		a.state.tx = tx
		a.state.startedAt = started.UTC()
		a.state.databaseDeadline = started.Add(remaining).UTC()
		a.state.mu.Unlock()
		outcome := body(ctx, a)
		a.state.mu.Lock()
		defer a.state.mu.Unlock()
		if ctx.Err() != nil || !a.state.finished || a.state.outcome != outcome {
			return ErrUnavailable
		}
		if a.state.failed != nil {
			return a.state.failed
		}
		switch outcome {
		case Success, CounterOnlyDenied:
			return nil
		case DeniedRollback:
			return ErrDenied
		default:
			return ErrUnavailable
		}
	})
	if err != nil {
		if errors.Is(err, ErrDenied) {
			return Completion{}, ErrDenied
		}
		return Completion{}, ErrUnavailable
	}
	// RuntimeDB returned only after Commit. Cancellation still suppresses output.
	if ctx.Err() != nil {
		return Completion{}, ErrUnavailable
	}
	return Completion{state: &completionState{binding: token, outcome: a.state.outcome}}, nil
}

func (b Binding) Matches(a *Attempt) bool {
	return b.state != nil && a != nil && a.state != nil && b.state == a.state.token
}
func (c Completion) Matches(a *Attempt) bool {
	return c.state != nil && Binding{state: c.state.binding}.Matches(a)
}
func (c Completion) Outcome() (Outcome, error) {
	if c.state == nil || c.state.binding == nil || !committable(c.state.outcome) {
		return 0, ErrClosed
	}
	return c.state.outcome, nil
}
func committable(o Outcome) bool { return o == Success || o == CounterOnlyDenied }
func (c Completion) TakeRelease(binding Binding) (Release, error) {
	if _, err := c.Outcome(); err != nil {
		return Release{}, err
	}
	if binding.state == nil || c.state.binding != binding.state {
		return Release{}, ErrUnrooted
	}
	if !c.state.used.CompareAndSwap(false, true) {
		return Release{}, ErrClosed
	}
	return Release{data: &releaseData{binding: binding.state, outcome: c.state.outcome}}, nil
}
func (r Release) Matches(binding Binding) bool {
	return r.data != nil && binding.state != nil && r.data.binding == binding.state
}
func (r Release) Outcome() (Outcome, error) {
	if r.data == nil || r.data.binding == nil || !committable(r.data.outcome) {
		return 0, ErrClosed
	}
	return r.data.outcome, nil
}
