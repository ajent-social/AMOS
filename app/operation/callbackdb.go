package operation

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var (
	errCallbackClosed     = errors.New("callback database closed")
	errCallbackBusy       = errors.New("callback database busy")
	errCallbackUnfinished = errors.New("callback database unfinished work")
	errCallbackCleanup    = errors.New("callback database cleanup failed")
	errCallbackSQL        = errors.New("callback database unsupported SQL")
	errCallbackContext    = errors.New("callback database context required")
	errCallbackScanner    = errors.New("callback scanner panicked")
)

// callbackDB deliberately exposes only DBTX. Lifecycle belongs to the separate
// owner closure; neither the transaction nor an unwrap path is exposed.
type callbackDB struct{ state *callbackState }

type callbackState struct {
	mu      sync.Mutex
	db      DBTX
	closed  bool
	drained chan struct{}
	cleanup error
	lease   *callbackLease
}

// active is non-nil only while an admitted method owns raw. Closing active
// hands exclusive access to invalidation. No driver or user code runs under mu.
type callbackLease struct {
	state    *callbackState
	cancel   context.CancelFunc
	active   chan struct{}
	raw      Rows
	finished bool
	terminal error
	busy     bool
}

type callbackRows struct{ lease *callbackLease }
type callbackRow struct {
	state    *callbackState
	lease    *callbackLease
	err      error
	consumed bool
}

func newCallbackDB(tx *sql.Tx) (DBTX, func() error, error) {
	db, err := newSQLTxAdapter(tx)
	if err != nil {
		return nil, nil, err
	}
	s := &callbackState{db: db, drained: make(chan struct{})}
	return &callbackDB{state: s}, s.invalidate, nil
}

func (s *callbackState) admit(ctx context.Context, query string) (*callbackLease, context.Context, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, nil, errCallbackClosed
	}
	if ctx == nil {
		return nil, nil, errCallbackContext
	}
	if !callbackSQL(query) {
		return nil, nil, errCallbackSQL
	}
	child, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	var err error
	if s.closed {
		err = errCallbackClosed
	} else if s.lease != nil {
		err = errCallbackBusy
	}
	if err != nil {
		s.mu.Unlock()
		cancel()
		return nil, nil, err
	}
	l := &callbackLease{state: s, cancel: cancel, active: make(chan struct{})}
	s.lease = l
	s.mu.Unlock()
	return l, child, nil
}

func (db *callbackDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	l, child, err := db.state.admit(ctx, query)
	if err != nil {
		return nil, err
	}
	defer l.leave(true)
	return db.state.db.ExecContext(child, query, args...)
}

func (db *callbackDB) query(ctx context.Context, query string, args ...any) (*callbackLease, error) {
	l, child, err := db.state.admit(ctx, query)
	if err != nil {
		return nil, err
	}
	raw, err := db.state.db.QueryContext(child, query, args...)
	if err != nil {
		l.leave(true)
		return nil, err
	}
	l.raw = raw
	l.leave(false)
	return l, nil
}

func (db *callbackDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	l, err := db.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &callbackRows{lease: l}, nil
}

func (db *callbackDB) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	l, err := db.query(ctx, query, args...)
	return &callbackRow{state: db.state, lease: l, err: err}
}

func (l *callbackLease) enter() error {
	s := l.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errCallbackClosed
	}
	if l.active != nil {
		l.busy = true
		return errCallbackBusy
	}
	l.active = make(chan struct{})
	return nil
}

func (l *callbackLease) leave(finish bool) {
	if finish {
		l.cancel()
	}
	s := l.state
	s.mu.Lock()
	if finish {
		l.finished = true
		if s.lease == l {
			s.lease = nil
		}
	}
	close(l.active)
	l.active = nil
	s.mu.Unlock()
}

// closeRaw is called only by the admitted method or after invalidation has
// joined that method. Clear raw before Close so cleanup is performed once.
func (l *callbackLease) closeRaw() error {
	if l.raw == nil {
		return nil
	}
	raw := l.raw
	l.raw = nil
	err := raw.Close()
	if err != nil {
		l.state.mu.Lock()
		l.state.cleanup = errors.Join(l.state.cleanup, errCallbackCleanup)
		l.state.mu.Unlock()
	}
	return err
}

func (s *callbackState) invalidate() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.drained
		return s.cleanup
	}
	s.closed = true
	l := s.lease
	var active chan struct{}
	if l != nil {
		s.cleanup = errors.Join(s.cleanup, errCallbackUnfinished)
		active = l.active
	}
	s.mu.Unlock()
	if l != nil {
		l.cancel()
		if active != nil {
			<-active
		}
		// No new result method can enter after closed was set.
		_ = l.closeRaw() // represented by the safe cleanup sentinel
		l.cancel()
	}
	s.mu.Lock()
	s.lease = nil
	close(s.drained)
	err := s.cleanup
	s.mu.Unlock()
	return err
}

func (r *callbackRows) Next() bool {
	l := r.lease
	if l.enter() != nil {
		return false
	}
	finish := l.finished
	defer func() { l.leave(finish) }()
	if finish {
		return false
	}
	if l.raw.Next() {
		return true
	}
	l.terminal = l.raw.Err()
	if err := l.closeRaw(); l.terminal == nil {
		l.terminal = err
	}
	finish = true
	return false
}

func (r *callbackRows) Scan(dest ...any) error {
	l := r.lease
	if err := l.enter(); err != nil {
		return err
	}
	finish := l.finished
	defer func() { l.leave(finish) }()
	if finish {
		return sql.ErrNoRows
	}
	value, panicked, err := callbackScan(l.raw, dest)
	if panicked {
		_ = l.closeRaw()
		finish = true
		panic(value)
	}
	return err
}

func (r *callbackRows) Err() error {
	l := r.lease
	if err := l.enter(); err != nil {
		return err
	}
	defer l.leave(false)
	l.state.mu.Lock()
	busy := l.busy
	l.state.mu.Unlock()
	if busy {
		return errCallbackBusy
	}
	if l.finished {
		return l.terminal
	}
	return l.raw.Err()
}

func (r *callbackRows) Close() error {
	l := r.lease
	if err := l.enter(); err != nil {
		return err
	}
	defer l.leave(true)
	if l.finished {
		return nil
	}
	err := l.closeRaw()
	if l.terminal == nil {
		l.terminal = err
	}
	return err
}

func (r *callbackRow) Scan(dest ...any) error {
	s := r.state
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errCallbackClosed
	}
	if r.lease == nil {
		if r.consumed {
			s.mu.Unlock()
			return sql.ErrNoRows
		}
		r.consumed = true
		err := r.err
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	l := r.lease
	if err := l.enter(); err != nil {
		return err
	}
	defer l.leave(true)
	if l.finished {
		return sql.ErrNoRows
	}
	defer func() { _ = l.closeRaw() }()
	for _, d := range dest {
		if _, ok := d.(*sql.RawBytes); ok {
			return errors.New("sql: RawBytes isn't allowed on Row.Scan")
		}
	}
	if !l.raw.Next() {
		if err := l.raw.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	value, panicked, err := callbackScan(l.raw, dest)
	if panicked {
		panic(value)
	}
	if err != nil {
		return err
	}
	return l.closeRaw()
}

// Shield inside sql.Scanner.Scan, so database/sql releases its internal scan
// lock before we close rows and rethrow the caller's exact panic value.
type callbackScanner struct {
	scanner  sql.Scanner
	value    any
	panicked bool
}

func (s *callbackScanner) Scan(src any) (err error) {
	defer func() {
		if p := recover(); p != nil {
			s.value, s.panicked, err = p, true, errCallbackScanner
		}
	}()
	return s.scanner.Scan(src)
}

func callbackScan(raw Rows, dest []any) (any, bool, error) {
	wrapped := append([]any(nil), dest...)
	var shields []*callbackScanner
	for i, d := range dest {
		if scanner, ok := d.(sql.Scanner); ok {
			shield := &callbackScanner{scanner: scanner}
			shields = append(shields, shield)
			wrapped[i] = shield
		}
	}
	err := raw.Scan(wrapped...)
	for _, s := range shields {
		if s.panicked {
			return s.value, true, err
		}
	}
	return nil, false, err
}
