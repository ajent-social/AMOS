package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/examples/continuity/domain"
)

// These scripted rows test the repository protocol, not live locks or authority.
type prelockDB struct {
	transform func(string, []any)
	invocationDB
	t                   *testing.T
	roots               int
	alter               func(*prelockDB, string, []any) error
	revision            int64
	sources             []string
	isolation, readOnly string
	draftExists         bool
}

const prelockProperty = "01900000-0000-7000-8000-000000000003"
const prelockSource = "01900000-0000-7000-8000-000000000002"
const prelockRoot = "01900000-0000-7000-8000-000000000009"

func newPrelockDB(t *testing.T) *prelockDB {
	return &prelockDB{t: t, revision: 1, sources: []string{prelockSource}, isolation: "read committed", readOnly: "off"}
}
func (d *prelockDB) QueryRowContext(ctx context.Context, q string, args ...any) operation.Row {
	d.record(ctx, q, args)
	return invocationRow(func(dest ...any) error {
		if d.alter != nil {
			if err := d.alter(d, q, args); err != nil {
				return err
			}
		}
		var values []any
		switch {
		case strings.Contains(q, "current_setting"):
			values = []any{d.isolation, d.readOnly}
		case strings.HasPrefix(q, "SELECT id FROM") || strings.HasPrefix(q, "SELECT case_id FROM"):
			if strings.Contains(q, "continuity_drafts") && !d.draftExists {
				return sql.ErrNoRows
			}
			values = []any{args[4].(string)}
		case strings.Contains(q, "FROM public.continuity_cases"):
			d.roots++
			raw, err := json.Marshal(d.sources)
			if err != nil {
				return err
			}
			values = []any{prelockRoot, prelockProperty, "Case", domain.AwaitingOwner, d.revision, raw}
		case strings.Contains(q, "FROM public.continuity_applications"):
			d.roots++
			raw, err := json.Marshal([]domain.ChecklistItem{{ID: prelockSource, Label: "Decision", HumanDecision: true}})
			if err != nil {
				return err
			}
			values = []any{prelockRoot, prelockProperty, d.revision, raw}
		case strings.Contains(q, "FROM public.continuity_procedures"):
			d.roots++
			values = []any{prelockRoot, "Procedure", "Body", d.revision}
		case strings.Contains(q, "FROM public.continuity_properties"):
			values = []any{prelockProperty, "Property", "Area", "Owner", "Occupant", "occupied", sql.NullTime{}}
		case strings.Contains(q, "FROM public.continuity_sources"):
			sum := sha256.Sum256([]byte("Body"))
			values = []any{args[4].(string), "document", "Source", "Body", hex.EncodeToString(sum[:])}
		default:
			d.t.Fatalf("unexpected query: %s", q)
		}
		if d.transform != nil {
			d.transform(q, values)
		}
		if len(values) != len(dest) {
			d.t.Fatalf("row arity: %d != %d", len(values), len(dest))
		}
		for i, v := range values {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
		}
		return nil
	})
}
func prelockRepo(t *testing.T, d *prelockDB) *Repository {
	t.Helper()
	r, err := NewWithDBTX(d, invocationScope())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestPrelockPureOrderAndScope(t *testing.T) {
	for _, action := range []string{"case.changed", "draft.saved", "checklist.changed", "procedure.changed"} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/draft=%t", action, exists), func(t *testing.T) {
				d := newPrelockDB(t)
				d.draftExists = exists
				r := prelockRepo(t, d)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				got, err := r.PrelockMutation(ctx, action, prelockRoot)
				if err != nil {
					t.Fatal(err)
				}
				want := []LockedResource{{"source", prelockSource}, {"property", prelockProperty}, {"case", prelockRoot}}
				switch action {
				case "draft.saved":
					want = append(want, LockedResource{"draft", prelockRoot})
				case "checklist.changed":
					want = []LockedResource{{"property", prelockProperty}, {"application", prelockRoot}}
				case "procedure.changed":
					want = []LockedResource{{"procedure", prelockRoot}}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("global order/closure: got %v want %v", got, want)
				}
				if d.roots != 2 {
					t.Fatalf("root snapshots=%d", d.roots)
				}
				locks := 0
				for i, c := range d.calls {
					if c.ctx != ctx {
						t.Fatal("context replaced")
					}
					if i == 0 {
						continue
					}
					if !strings.Contains(c.query, scoped) || !reflect.DeepEqual(c.args[:4], r.args()[:4]) {
						t.Fatal("scope not retained")
					}
					locking := strings.Contains(c.query, " FOR ")
					if i == 1 && locking {
						t.Fatal("discovery took a lock")
					}
					if i >= 2 && i < 2+len(want) {
						if !locking || c.args[4] != want[i-2].ID {
							t.Fatal("global lock order differs")
						}
						locks++
						mode := " FOR UPDATE"
						if want[i-2].Kind == "property" || want[i-2].Kind == "source" {
							mode = " FOR SHARE"
						}
						if !strings.HasSuffix(c.query, mode) {
							t.Fatal("lock mode differs")
						}
					} else if locking {
						t.Fatal("late lock acquired")
					}
				}
				if locks != len(want) {
					t.Fatal("incomplete locks")
				}
				got[0].ID = "changed"
				again, e := r.PrelockMutation(ctx, action, prelockRoot)
				if e != nil || again[0].ID != want[0].ID {
					t.Fatal("returned observations alias retained state")
				}
			})
		}
	}
}
func TestPrelockPureChangedClosure(t *testing.T) {
	for _, action := range []string{"case.changed", "draft.saved", "checklist.changed", "procedure.changed"} {
		for _, change := range []string{"revision", "relation"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				d := newPrelockDB(t)
				d.alter = func(d *prelockDB, q string, _ []any) error {
					if d.roots == 1 && !strings.HasPrefix(q, "SELECT id FROM") && !strings.HasPrefix(q, "SELECT case_id FROM") {
						if change == "relation" && (action == "case.changed" || action == "draft.saved") {
							d.sources = []string{testID}
						} else {
							d.revision++
						}
					}
					return nil
				}
				got, err := prelockRepo(t, d).PrelockMutation(context.Background(), action, prelockRoot)
				if got != nil || err != ErrConflict {
					t.Fatalf("stale closure admitted: %v %v", got, err)
				}
				for _, c := range d.calls {
					if len(c.args) > 4 && c.args[4] == testID {
						t.Fatal("new lower relation queried or locked")
					}
				}
			})
		}
	}
}
func TestPrelockPureFailures(t *testing.T) {
	for _, mode := range []string{"isolation", "readonly", "mode-error", "discovery-missing", "lock-missing", "lock-error", "reread-error", "property-error", "source-error", "cancel-lock", "cancel-final"} {
		t.Run(mode, func(t *testing.T) {
			d := newPrelockDB(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrUnavailable
			if mode == "isolation" {
				d.isolation = "repeatable read"
			}
			if mode == "readonly" {
				d.readOnly = "on"
			}
			d.alter = func(d *prelockDB, q string, _ []any) error {
				fail := false
				switch mode {
				case "mode-error":
					fail = strings.Contains(q, "current_setting")
				case "discovery-missing":
					if strings.Contains(q, "SELECT id,property_id,title") {
						return sql.ErrNoRows
					}
				case "lock-missing":
					if strings.HasPrefix(q, "SELECT id FROM") {
						return sql.ErrNoRows
					}
				case "lock-error":
					fail = strings.HasPrefix(q, "SELECT id FROM")
				case "reread-error":
					fail = d.roots == 1 && strings.Contains(q, "SELECT id,property_id,title")
				case "property-error":
					fail = strings.HasPrefix(q, "SELECT id,name")
				case "source-error":
					fail = strings.HasPrefix(q, "SELECT id,kind")
				case "cancel-lock":
					if strings.HasPrefix(q, "SELECT id FROM") {
						cancel()
					}
				case "cancel-final":
					if strings.HasPrefix(q, "SELECT id,name") {
						cancel()
					}
				}
				if fail {
					return errors.New("unavailable adapter")
				}
				return nil
			}
			if strings.HasSuffix(mode, "missing") {
				want = ErrNotFound
			}
			got, err := prelockRepo(t, d).PrelockMutation(ctx, "case.changed", prelockRoot)
			if got != nil || err != want {
				t.Fatalf("failure disclosed result: %v %v", got, err)
			}
			if (mode == "readonly" || mode == "isolation" || mode == "mode-error") && len(d.calls) != 1 {
				t.Fatal("mode failure touched domain")
			}
		})
	}
	d := newPrelockDB(t)
	r := prelockRepo(t, d)
	for _, sel := range [][2]string{{"unknown", prelockRoot}, {"case.changed", "bad"}} {
		v, e := r.PrelockMutation(context.Background(), sel[0], sel[1])
		if v != nil || e != ErrInvalid {
			t.Fatal("invalid selector admitted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		v, e := r.PrelockMutation(ctx, "case.changed", prelockRoot)
		if v != nil || e != ErrUnavailable {
			t.Fatal("unavailable context admitted")
		}
	}
	if len(d.calls) != 0 {
		t.Fatal("invalid input performed SQL")
	}
}
func TestPrelockPureBoundsAndTies(t *testing.T) {
	c := mutationSnapshot{caseValue: domain.Case{PropertyID: prelockRoot, SourceIDs: []string{prelockRoot}}}
	got, e := mutationResources("draft.saved", prelockRoot, c)
	want := []LockedResource{{"property", prelockRoot}, {"source", prelockRoot}, {"case", prelockRoot}, {"draft", prelockRoot}}
	if e != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("same UUID kind order differs")
	}
	c.caseValue.SourceIDs = []string{prelockSource, prelockSource}
	if v, e := mutationResources("case.changed", prelockRoot, c); v != nil || e != ErrUnavailable {
		t.Fatal("duplicate tuple admitted")
	}
	c.caseValue.SourceIDs = nil
	for i := 0; i < 32; i++ {
		c.caseValue.SourceIDs = append(c.caseValue.SourceIDs, fmt.Sprintf("01900000-0000-7000-8000-%012d", 100+i))
	}
	if v, e := mutationResources("draft.saved", prelockRoot, c); e != nil || len(v) != 35 {
		t.Fatal("maximum closure rejected")
	}
	c.caseValue.SourceIDs = append(c.caseValue.SourceIDs, testID)
	if v, e := mutationResources("draft.saved", prelockRoot, c); v != nil || e != ErrUnavailable {
		t.Fatal("oversized closure admitted")
	}
}
func TestPrelockPureLegacyReadLocks(t *testing.T) {
	d := newPrelockDB(t)
	if _, err := prelockRepo(t, d).Case(context.Background(), prelockRoot); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 3 || strings.Contains(d.calls[0].query, " FOR ") || !strings.Contains(d.calls[1].query, "continuity_properties") || !strings.HasSuffix(d.calls[2].query, " FOR SHARE") {
		t.Fatal("legacy case relation order or locking changed")
	}
}

func TestPrelockPureMalformedRows(t *testing.T) {
	for _, kind := range []string{"source digest", "case revision", "case sources", "property", "procedure", "application"} {
		t.Run(kind, func(t *testing.T) {
			d := newPrelockDB(t)
			action := "case.changed"
			if kind == "procedure" {
				action = "procedure.changed"
			}
			if kind == "application" {
				action = "checklist.changed"
			}
			d.transform = func(q string, values []any) {
				switch {
				case kind == "source digest" && strings.HasPrefix(q, "SELECT id,kind"):
					values[4] = strings.Repeat("0", 64)
				case kind == "case revision" && strings.HasPrefix(q, "SELECT id,property_id,title"):
					values[4] = int64(0)
				case kind == "case sources" && strings.HasPrefix(q, "SELECT id,property_id,title"):
					values[5] = []byte(`[]`)
				case kind == "property" && strings.HasPrefix(q, "SELECT id,name"):
					values[5] = "invalid"
				case kind == "procedure" && strings.HasPrefix(q, "SELECT id,title"):
					values[3] = int64(0)
				case kind == "application" && strings.HasPrefix(q, "SELECT id,property_id,revision"):
					values[3] = []byte(`[]`)
				}
			}
			got, e := prelockRepo(t, d).PrelockMutation(context.Background(), action, prelockRoot)
			if got != nil || e != ErrUnavailable {
				t.Fatalf("malformed persisted row admitted: %v %v", got, e)
			}
		})
	}
}
