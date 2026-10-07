package apphost

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

var bindingRealm = [3]uuid.UUID{
	uuid.MustParse("01900000-0000-7000-8000-000000000001"),
	uuid.MustParse("01900000-0000-7000-8000-000000000002"),
	uuid.MustParse("01900000-0000-7000-8000-000000000003"),
}

type bindingRunnerFunc func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error

func (f bindingRunnerFunc) WithTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	return f(ctx, opts, fn)
}

type bindingNilMap map[string]string
type bindingNilSlice []string
type bindingNilChannel chan string

func (bindingNilMap) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil map runner called")
}
func (bindingNilSlice) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil slice runner called")
}
func (bindingNilChannel) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	panic("nil channel runner called")
}

type bindingValueRunner struct{ calls *int }

func (r bindingValueRunner) WithTx(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
	*r.calls++
	return errors.New("private runner detail")
}

func TestRuntimeBindingAdmission(t *testing.T) {
	for name, runner := range map[string]storage.TxRunner{
		"interface": nil, "runtime pointer": (*storage.RuntimeDB)(nil),
		"development pointer": (*storage.DB)(nil), "function": bindingRunnerFunc(nil),
		"map": bindingNilMap(nil), "slice": bindingNilSlice(nil), "channel": bindingNilChannel(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if err := runtimeBindingReady(context.Background(), runner, bindingRealm[0], bindingRealm[1], bindingRealm[2]); err != errRuntimeBindingConfiguration {
				t.Fatalf("nil runner: %v", err)
			}
		})
	}
	calls := 0
	runner := bindingValueRunner{&calls}
	if err := runtimeBindingReady(nil, runner, bindingRealm[0], bindingRealm[1], bindingRealm[2]); err != errRuntimeBindingConfiguration || calls != 0 {
		t.Fatal("nil context reached runner")
	}
	for i := range bindingRealm {
		for _, invalid := range []uuid.UUID{uuid.Nil, uuid.MustParse("01900000-0000-4000-8000-000000000001"), uuid.MustParse("01900000-0000-7000-0000-000000000001")} {
			ids := bindingRealm
			ids[i] = invalid
			if err := runtimeBindingReady(context.Background(), runner, ids[0], ids[1], ids[2]); err != errRuntimeBindingConfiguration || calls != 0 {
				t.Fatal("invalid realm reached runner")
			}
		}
	}
	if err := runtimeBindingReady(context.Background(), runner, bindingRealm[0], bindingRealm[1], bindingRealm[2]); err != errRuntimeBindingUnavailable || calls != 1 {
		t.Fatal("non-nil value runner was not admitted")
	}
}

func TestRuntimeBindingOptionsAndErrors(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(map[bool]string{false: "bounded", true: "earlier deadline"}[earlier], func(t *testing.T) {
			ctx := context.Background()
			if earlier {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			calls := 0
			before := time.Now()
			var operation context.Context
			runner := bindingRunnerFunc(func(got context.Context, opts *sql.TxOptions, _ func(*sql.Tx) error) error {
				calls++
				operation = got
				if opts == nil || !opts.ReadOnly || opts.Isolation != sql.LevelReadCommitted {
					t.Fatal("wrong transaction options")
				}
				deadline, ok := got.Deadline()
				if !ok || deadline.After(time.Now().Add(2*time.Second)) || deadline.Before(before) {
					t.Fatal("missing bounded deadline")
				}
				if earlier {
					want, _ := ctx.Deadline()
					if !deadline.Equal(want) {
						t.Fatal("caller deadline extended")
					}
				}
				return errors.New("PRIVATE DATABASE DETAIL")
			})
			err := runtimeBindingReady(ctx, runner, bindingRealm[0], bindingRealm[1], bindingRealm[2])
			if err != errRuntimeBindingUnavailable || calls != 1 || strings.Contains(err.Error(), "PRIVATE") || operation.Err() != context.Canceled {
				t.Fatal("unsafe failure or unbounded context lifetime")
			}
		})
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		ctx, cancel := context.WithCancel(context.Background())
		if cause == context.DeadlineExceeded {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			cancel()
		}
		runner := bindingRunnerFunc(func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { return errors.New("private") })
		err := runtimeBindingReady(ctx, runner, bindingRealm[0], bindingRealm[1], bindingRealm[2])
		cancel()
		if !errors.Is(err, errRuntimeBindingUnavailable) || !errors.Is(err, cause) {
			t.Fatalf("context cause lost: %v", err)
		}
	}
}

func TestRuntimeBindingCannotInventSuccess(t *testing.T) {
	for name, runner := range map[string]bindingRunnerFunc{
		"no callback":     func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error { return nil },
		"nil transaction": func(_ context.Context, _ *sql.TxOptions, fn func(*sql.Tx) error) error { return fn(nil) },
		"foreign configuration error": func(context.Context, *sql.TxOptions, func(*sql.Tx) error) error {
			return errRuntimeBindingConfiguration
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runtimeBindingReady(context.Background(), runner, bindingRealm[0], bindingRealm[1], bindingRealm[2]); err != errRuntimeBindingUnavailable {
				t.Fatal("runner invented binding evidence")
			}
		})
	}
}
