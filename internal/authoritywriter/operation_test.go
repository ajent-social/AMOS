package authoritywriter

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
)

func operationPlanForTest(t *testing.T) (Plan, OperationPlan, []Row) {
	t.Helper()
	op := OperationPlan{testID(t), testID(t), testID(t), testID(t)}
	rows := []Row{{Persons, op.ActorID, ExistingUpdate}, {Sessions, op.SessionID, ExistingUpdate}, {Workspaces, op.WorkspaceID, ExistingUpdate}}
	p, err := NewOperationPlan(testRealm(t), rows, op)
	if err != nil {
		t.Fatal(err)
	}
	return p, op, rows
}

func TestOperationPlanExactInventory(t *testing.T) {
	p, op, rows := operationPlanForTest(t)
	saved := *p.data.operation
	op.InvocationID = testID(t)
	rows[0].ID = testID(t)
	if *p.data.operation != saved || p.data.rows[0].ID != saved.ActorID {
		t.Fatal("caller mutated copied plan")
	}
	membership := Row{Memberships, testID(t), ExistingUpdate}
	if _, err := NewOperationPlan(p.data.realm, append(append([]Row(nil), p.data.rows...), membership), saved); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zero invocation", "invalid realm", "missing person", "wrong person", "wrong session", "wrong workspace", "reserved session", "shared workspace", "foreign table", "two memberships", "invocation aliases row"} {
		t.Run(name, func(t *testing.T) {
			candidate := saved
			rr := append([]Row(nil), p.data.rows...)
			realm := p.data.realm
			switch name {
			case "zero invocation":
				candidate.InvocationID = [16]byte{}
			case "invalid realm":
				realm = Realm{}
			case "missing person":
				rr = rr[1:]
			case "wrong person":
				candidate.ActorID = testID(t)
			case "wrong session":
				candidate.SessionID = testID(t)
			case "wrong workspace":
				candidate.WorkspaceID = testID(t)
			case "reserved session":
				rr[1].Access = ReservedInsert
			case "shared workspace":
				rr[2].Access = ExistingShare
			case "foreign table":
				rr = append(rr, Row{Factors, testID(t), ExistingUpdate})
			case "two memberships":
				rr = append(rr, membership, Row{Memberships, testID(t), ExistingUpdate})
			case "invocation aliases row":
				candidate.InvocationID = rr[0].ID
			}
			got, err := NewOperationPlan(realm, rr, candidate)
			if !errors.Is(err, ErrUnrooted) || got.data != nil {
				t.Fatal("invalid operation inventory accepted")
			}
		})
	}
}

// syntheticOperationAttempt exercises only in-memory ordering and rejection
// guards. The nonnil sql.Tx is never allowed to execute SQL; it does not prove
// native admission, database locking, constraint drain, commit or authority.
func syntheticOperationAttempt(t *testing.T) (*Attempt, context.Context, context.CancelFunc) {
	t.Helper()
	p, _, _ := operationPlanForTest(t)
	r := &rootState{runtime: &storage.RuntimeDB{}}
	profile.Lock()
	oldRoot, oldLegacy := profile.root, profile.legacy
	profile.root, profile.legacy = r, false
	profile.Unlock()
	t.Cleanup(func() { profile.Lock(); profile.root, profile.legacy = oldRoot, oldLegacy; profile.Unlock() })
	token := &identityToken{nonzero: 1}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), requestKey{}, token), time.Minute)
	t.Cleanup(cancel)
	end, _ := ctx.Deadline()
	a := &Attempt{state: &attemptState{root: r, token: token, tx: &sql.Tx{}, deadline: end, done: ctx.Done(), live: true, plan: p.data, phase: W}}
	return a, ctx, cancel
}

func runOperationSteps(t *testing.T, a *Attempt, ctx context.Context, steps ...OperationStep) {
	t.Helper()
	for _, step := range steps {
		tx, err := a.OperationTx(ctx, step)
		if err != nil || tx != a.state.tx {
			t.Fatalf("step %d did not retain exact transaction: %v", step, err)
		}
		if a.state.phase != D {
			t.Fatal("step left existing D phase")
		}
		if err := a.CompleteOperationStep(ctx, step); err != nil {
			t.Fatal(err)
		}
	}
}
func TestOperationJournalLegalPathsAndOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []OperationStep
		out   Outcome
	}{
		{"new", []OperationStep{OperationAuthority, OperationResources, OperationClaim, OperationCallback, OperationResult, OperationAudit}, Success},
		{"replay", []OperationStep{OperationAuthority, OperationResources, OperationClaim, OperationReplay}, Success},
		{"authority denied", []OperationStep{OperationAuthority, OperationDeniedAudit}, OperationDeniedCommitted},
		{"resources denied", []OperationStep{OperationAuthority, OperationResources, OperationDeniedAudit}, OperationDeniedCommitted},
		{"authority unavailable", []OperationStep{OperationAuthority, OperationUnavailableAudit}, OperationUnavailableCommitted},
		{"resources unavailable", []OperationStep{OperationAuthority, OperationResources, OperationUnavailableAudit}, OperationUnavailableCommitted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, ctx, _ := syntheticOperationAttempt(t)
			runOperationSteps(t, a, ctx, tc.steps...)
			if !a.state.operationTerminal() {
				t.Fatal("completed path not terminal")
			}
			for _, out := range []Outcome{Success, CounterOnlyDenied, OperationDeniedCommitted, OperationUnavailableCommitted} {
				if got := a.state.operationOutcome(out); got != (out == tc.out) {
					t.Fatalf("wrong terminal outcome %d: %v", out, got)
				}
			}
			// F is set synthetically ONLY to test the pure terminal guard. This is
			// not evidence that DrainAndSample or a real transaction succeeded.
			a.state.phase = F
			a.state.sample = time.Now()
			if err := a.Finish(tc.out); err != nil {
				t.Fatal(err)
			}
			if _, err := a.OperationTx(ctx, OperationAuthority); !errors.Is(err, ErrClosed) {
				t.Fatal("finished attempt reopened")
			}
		})
	}
}

func TestOperationJournalRejectsIllegalPaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix []OperationStep
		next   OperationStep
	}{
		{"skip authority", nil, OperationResources},
		{"skip resources", []OperationStep{OperationAuthority}, OperationClaim},
		{"skip claim", []OperationStep{OperationAuthority, OperationResources}, OperationCallback},
		{"skip callback", []OperationStep{OperationAuthority, OperationResources, OperationClaim}, OperationResult},
		{"skip result", []OperationStep{OperationAuthority, OperationResources, OperationClaim, OperationCallback}, OperationAudit},
		{"post claim denial", []OperationStep{OperationAuthority, OperationResources, OperationClaim}, OperationDeniedAudit},
		{"post claim unavailable", []OperationStep{OperationAuthority, OperationResources, OperationClaim}, OperationUnavailableAudit},
		{"repeat authority", []OperationStep{OperationAuthority}, OperationAuthority},
		{"replay then callback", []OperationStep{OperationAuthority, OperationResources, OperationClaim, OperationReplay}, OperationCallback},
		{"deny then result", []OperationStep{OperationAuthority, OperationDeniedAudit}, OperationResult},
		{"unknown", nil, OperationStep(255)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, ctx, _ := syntheticOperationAttempt(t)
			runOperationSteps(t, a, ctx, tc.prefix...)
			if tx, err := a.OperationTx(ctx, tc.next); !errors.Is(err, ErrPhase) || tx != nil {
				t.Fatal("invalid path returned transaction")
			}
			if _, err := a.OperationTx(ctx, OperationAuthority); !errors.Is(err, ErrPhase) {
				t.Fatal("poisoned path recovered")
			}
		})
	}
}

func TestOperationJournalLifecycleAndMixedPaths(t *testing.T) {
	for _, name := range []string{"open step", "wrong completion", "repeat completion", "zero completion", "canceled", "wrong context", "closed", "legacy", "too early", "after F", "legacy mutation", "legacy participant", "legacy delivery", "counter", "pending drain", "unfinished drain", "wrong outcome", "success before F", "post claim rollback"} {
		t.Run(name, func(t *testing.T) {
			a, ctx, cancel := syntheticOperationAttempt(t)
			var err error
			switch name {
			case "open step":
				if _, e := a.OperationTx(ctx, OperationAuthority); e != nil {
					t.Fatal(e)
				}
				_, err = a.OperationTx(ctx, OperationResources)
			case "wrong completion":
				if _, e := a.OperationTx(ctx, OperationAuthority); e != nil {
					t.Fatal(e)
				}
				err = a.CompleteOperationStep(ctx, OperationResources)
			case "repeat completion":
				runOperationSteps(t, a, ctx, OperationAuthority)
				err = a.CompleteOperationStep(ctx, OperationAuthority)
			case "zero completion":
				err = a.CompleteOperationStep(ctx, 0)
			case "canceled":
				cancel()
				_, err = a.OperationTx(ctx, OperationAuthority)
			case "wrong context":
				_, err = a.OperationTx(context.Background(), OperationAuthority)
			case "closed":
				a.state.live = false
				_, err = a.OperationTx(ctx, OperationAuthority)
			case "legacy":
				a.state.plan.operation = nil
				_, err = a.OperationTx(ctx, OperationAuthority)
			case "too early":
				a.state.phase = S
				_, err = a.OperationTx(ctx, OperationAuthority)
			case "after F":
				a.state.phase = F
				_, err = a.OperationTx(ctx, OperationAuthority)
			case "legacy mutation":
				err = a.RecordMutation(WorkspaceWrite, []Row{{Workspaces, a.state.plan.operation.WorkspaceID, ExistingUpdate}})
			case "legacy participant":
				runOperationSteps(t, a, ctx, OperationAuthority)
				_, err = a.ParticipantTx(ctx, W, []Row{{Workspaces, a.state.plan.operation.WorkspaceID, ExistingUpdate}})
			case "legacy delivery":
				_, err = a.DeliveryTx(ctx, Delivery{}, MaterialInsert)
			case "counter":
				err = a.RestrictCounter(testID(t))
			case "pending drain":
				if _, e := a.OperationTx(ctx, OperationAuthority); e != nil {
					t.Fatal(e)
				}
				_, err = a.DrainAndSample(ctx)
			case "unfinished drain":
				_, err = a.DrainAndSample(ctx)
			case "wrong outcome":
				runOperationSteps(t, a, ctx, OperationAuthority, OperationDeniedAudit)
				a.state.phase = F
				a.state.sample = time.Now()
				err = a.Finish(Success)
			case "success before F":
				runOperationSteps(t, a, ctx, OperationAuthority, OperationResources, OperationClaim, OperationReplay)
				err = a.Finish(Success)
			case "post claim rollback":
				runOperationSteps(t, a, ctx, OperationAuthority, OperationResources, OperationClaim)
				if e := a.Finish(DeniedRollback); e != nil {
					t.Fatal(e)
				}
				if a.state.outcome != DeniedRollback || !a.state.finished {
					t.Fatal("rollback marker lost")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid lifecycle accepted")
			}
		})
	}
}

func TestOperationAuditReleasesRemainBoundAndSingleUse(t *testing.T) {
	for _, out := range []Outcome{OperationDeniedCommitted, OperationUnavailableCommitted} {
		a, _, _ := syntheticOperationAttempt(t)
		// A fabricated completion tests only the release container, not commit.
		c := Completion{state: &completionState{binding: a.state.token, outcome: out}}
		binding := Binding{state: a.state.token}
		if _, err := c.TakeRelease(Binding{state: &identityToken{nonzero: 1}}); !errors.Is(err, ErrUnrooted) {
			t.Fatal("foreign release binding accepted")
		}
		release, err := c.TakeRelease(binding)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := release.Outcome()
		if err != nil || actual != out || !release.Matches(binding) {
			t.Fatal("committed outcome lost")
		}
		if _, err := c.TakeRelease(binding); !errors.Is(err, ErrClosed) {
			t.Fatal("second release accepted")
		}
	}
}

func TestOperationProfileDoesNotChangeLegacyPlan(t *testing.T) {
	p, _, _ := operationPlanForTest(t)
	legacy, err := NewPlan(p.data.realm, p.data.rows, nil)
	if err != nil || legacy.data.operation != nil || !reflect.DeepEqual(legacy.data.rows, p.data.rows) {
		t.Fatal("legacy plan changed")
	}
	for _, out := range []Outcome{OperationDeniedCommitted, OperationUnavailableCommitted} {
		a, _, _ := syntheticOperationAttempt(t)
		a.state.plan.operation = nil
		a.state.phase = F
		a.state.sample = time.Now()
		if err := a.Finish(out); !errors.Is(err, ErrPhase) {
			t.Fatal("legacy plan committed operation outcome")
		}
	}
}
