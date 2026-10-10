package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ajent-social/amos/app/operation"
	"github.com/ajent-social/amos/examples/continuity/domain"
)

const draftOldSource = "01900000-0000-7000-8000-000000000005"

type draftDependencyDB struct {
	invocationDB
	absentDraft, absentCase, absentHistorical bool
}

func draftRow(values ...any) operation.Row {
	return invocationRow(func(dest ...any) error {
		if len(dest) != len(values) {
			return fmt.Errorf("unexpected column count")
		}
		for i, v := range values {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
		}
		return nil
	})
}
func (db *draftDependencyDB) QueryRowContext(ctx context.Context, query string, args ...any) operation.Row {
	db.record(ctx, query, args)
	absent := invocationRow(func(...any) error { return sql.ErrNoRows })
	switch {
	case strings.Contains(query, "FROM public.continuity_drafts"):
		if db.absentDraft {
			return absent
		}
		return draftRow(testID, int64(1), "Saved unsent text", []byte(`["`+draftOldSource+`"]`))
	case strings.Contains(query, "FROM public.continuity_cases"):
		if db.absentCase {
			return absent
		}
		return draftRow(testID, testID, "Current case", domain.InProgress, int64(2), []byte(`["`+testID+`"]`))
	case strings.Contains(query, "FROM public.continuity_properties"):
		return draftRow(testID, "Property", "Area", "Owner", "Occupant", "occupied", sql.NullTime{})
	case strings.Contains(query, "FROM public.continuity_sources"):
		id, ok := args[4].(string)
		if !ok {
			panic("unexpected source selector")
		}
		if id == draftOldSource && db.absentHistorical {
			return absent
		}
		body := "Synthetic source text"
		sum := sha256.Sum256([]byte(body))
		return draftRow(id, "document", "Source", body, hex.EncodeToString(sum[:]))
	default:
		panic("unexpected draft read query")
	}
}

func TestDraftMissingDependencyIsUnavailable(t *testing.T) {
	for _, test := range []struct {
		name                                         string
		missingDraft, missingCase, missingHistorical bool
		want                                         error
	}{
		{"absent saved row", true, false, false, ErrNotFound},
		{"existing row missing case", false, true, false, ErrUnavailable},
		{"existing stale row missing historical source", false, false, true, ErrUnavailable},
		{"existing stale row intact", false, false, false, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &draftDependencyDB{absentDraft: test.missingDraft, absentCase: test.missingCase, absentHistorical: test.missingHistorical}
			r, err := NewWithDBTX(db, invocationScope())
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Draft(context.Background(), testID)
			if err != test.want {
				t.Fatalf("draft classification: got %v, want %v", err, test.want)
			}
			if test.want != nil {
				if !reflect.DeepEqual(got, domain.Draft{}) {
					t.Fatal("failed snapshot disclosed partial draft")
				}
			} else if got.CaseRevision != 1 || got.Body != "Saved unsent text" || !reflect.DeepEqual(got.SourceIDs, []string{draftOldSource}) {
				t.Fatal("historical snapshot rebound to current case")
			}
			if test.missingDraft && len(db.calls) != 1 {
				t.Fatal("absent draft performed dependency reads")
			}
		})
	}
}
