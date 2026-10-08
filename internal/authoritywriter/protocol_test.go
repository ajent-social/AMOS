package authoritywriter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func testRealm(t *testing.T) Realm { return Realm{testID(t), testID(t), testID(t)} }

func TestPlanCopiesOrdersAndRejectsAmbiguity(t *testing.T) {
	realm := testRealm(t)
	p1, p2 := testID(t), testID(t)
	rows := []Row{{Sessions, testID(t), ReservedInsert}, {Persons, p2, ExistingUpdate}, {Persons, p1, ExistingUpdate}, {Connections, testID(t), ExistingShare}}
	delivery := []Delivery{{testID(t), "purpose/request"}}
	p, err := NewPlan(realm, rows, delivery)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].ID = testID(t)
	delivery[0].JobKey = "changed"
	if p.data.rows[0].ID != p1 || p.data.rows[1].ID != p2 || p.data.rows[2].Table != Connections || p.data.rows[3].Table != Sessions || p.data.delivery[0].JobKey != "purpose/request" {
		t.Fatal("plan did not copy and order its inventory")
	}
	for _, tc := range []struct {
		name     string
		realm    Realm
		rows     []Row
		delivery []Delivery
	}{
		{"zero realm", Realm{}, nil, nil},
		{"unknown table", realm, []Row{{Table(200), p1, ExistingUpdate}}, nil},
		{"zero id", realm, []Row{{Persons, uuid.Nil, ExistingUpdate}}, nil},
		{"nonv7 id", realm, []Row{{Persons, uuid.New(), ExistingUpdate}}, nil},
		{"share person", realm, []Row{{Persons, p1, ExistingShare}}, nil},
		{"unknown access", realm, []Row{{Persons, p1, Access(200)}}, nil},
		{"duplicate access", realm, []Row{{Persons, p1, ExistingUpdate}, {Persons, p1, ReservedInsert}}, nil},
		{"multiple delivery", realm, nil, []Delivery{{testID(t), "a"}, {testID(t), "b"}}},
		{"empty key", realm, nil, []Delivery{{testID(t), ""}}},
		{"control key", realm, nil, []Delivery{{testID(t), "a\x00b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPlan(tc.realm, tc.rows, tc.delivery); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
}

func TestCompletionHasOneTerminalRelease(t *testing.T) {
	token := &identityToken{nonzero: 1}
	binding := Binding{state: token}
	a := &Attempt{state: &attemptState{token: token, live: false}}
	c := Completion{state: &completionState{binding: token, outcome: Success}}
	if !binding.Matches(a) || !c.Matches(a) {
		t.Fatal("closed attempt lost terminal identity")
	}
	if _, err := c.TakeRelease(Binding{state: &identityToken{nonzero: 1}}); !errors.Is(err, ErrUnrooted) {
		t.Fatal("foreign binding accepted")
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := c
			r, err := copy.TakeRelease(binding)
			if err == nil {
				winners.Add(1)
				if !r.Matches(binding) {
					t.Error("release binding lost")
				}
				if out, e := r.Outcome(); e != nil || out != Success {
					t.Error("release outcome lost")
				}
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("terminal releases = %d, want 1", winners.Load())
	}
	if _, err := a.Binding(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed attempt reopened")
	}
	if _, err := (Completion{}).TakeRelease(binding); err == nil {
		t.Fatal("zero completion published")
	}
	if _, err := (Release{}).Outcome(); err == nil {
		t.Fatal("zero release accepted")
	}
	for _, out := range []Outcome{0, DeniedRollback, UnavailableRollback, Outcome(99)} {
		if _, err := (Completion{state: &completionState{binding: token, outcome: out}}).Outcome(); err == nil {
			t.Fatal("noncommitted outcome admitted")
		}
	}
}

func TestRootBudgetPreservesEarlierDeadlineAndCancellation(t *testing.T) {
	start := time.Now()
	ctx, cancel, err := bounded(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	d, _ := ctx.Deadline()
	if d.Before(start.Add(rootBudget)) || d.After(time.Now().Add(rootBudget)) {
		t.Fatal("root budget not five seconds")
	}
	earlier, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	ctx2, cancel2, err := bounded(earlier)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()
	want, _ := earlier.Deadline()
	got, _ := ctx2.Deadline()
	if !got.Equal(want) {
		t.Fatal("earlier caller budget extended")
	}
	stop()
	if _, _, err := bounded(earlier); err == nil {
		t.Fatal("canceled request admitted")
	}
	var nilContext context.Context
	if _, _, err := bounded(nilContext); err == nil {
		t.Fatal("nil context admitted")
	}
}

func TestProfilesAreImmutableInSeparateProcesses(t *testing.T) {
	for _, mode := range []string{"legacy", "writer"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestProfileChild$")
			cmd.Env = append(os.Environ(), "AMOS_WRITER_PROFILE_TEST="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("profile child: %v\n%s", err, output)
			}
		})
	}
}
func TestProfileChild(t *testing.T) {
	mode := os.Getenv("AMOS_WRITER_PROFILE_TEST")
	if mode == "" {
		return
	}
	if _, err := New(nil); err == nil {
		t.Fatal("nil runtime accepted")
	}
	r, err := New(&storage.RuntimeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "legacy" {
		if err := SelectLegacy(); err != nil {
			t.Fatal(err)
		}
		if err := SelectLegacy(); err != nil {
			t.Fatal("legacy reuse refused")
		}
		if err := ActivateW1(r); err == nil {
			t.Fatal("legacy upgraded to W1")
		}
		return
	}
	if err := ActivateW1(r); err != nil {
		t.Fatal(err)
	}
	if err := SelectLegacy(); err == nil {
		t.Fatal("W1 downgraded")
	}
	if err := ActivateW1(r); err == nil {
		t.Fatal("duplicate activation accepted")
	}
	other, err := New(&storage.RuntimeDB{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ActivateW1(other); err == nil {
		t.Fatal("foreign root replaced active root")
	}
	called := false
	if _, err := r.Run(context.Background(), func(context.Context, *Attempt) Outcome { called = true; return Success }); err == nil || called {
		t.Fatal("closed runtime admitted callback")
	}
}
