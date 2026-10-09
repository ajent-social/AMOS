package baselineview

import (
	"encoding/base64"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/repository"
	"github.com/google/uuid"
)

func validID(s string) bool {
	if len(s) != 36 {
		return false
	}
	id, err := uuid.Parse(s)
	return err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == s
}
func textValue(s string, max int, body bool) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if body {
			if (r < 32 || r == 127) && r != '\t' && r != '\n' {
				return "", false
			}
		} else if unicode.IsControl(r) {
			return "", false
		}
	}
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n > 0 && n <= max
}
func rawIDs(ids []string) bool {
	if len(ids) > 32 {
		return false
	}
	for _, id := range ids {
		if len(id) > 36 {
			return false
		}
	}
	return true
}
func validIDs(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !validID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func rawCase(v domain.Case) bool {
	return len(v.ID) <= 36 && len(v.PropertyID) <= 36 && len(v.Title) <= 800 && len(v.Status) <= 24 && rawIDs(v.SourceIDs)
}
func caseValue(v domain.Case) (domain.Case, bool) {
	// PrepareDraft is a pure validation/copy operation, not a state mutation.
	d, err := domain.PrepareDraft(v, "Validation only")
	if err != nil {
		return domain.Case{}, false
	}
	v.Title = strings.TrimSpace(v.Title)
	v.SourceIDs = d.SourceIDs
	return v, true
}
func rawApplication(v domain.Application) bool {
	if len(v.ID) > 36 || len(v.PropertyID) > 36 || len(v.Items) > 32 {
		return false
	}
	for _, item := range v.Items {
		if len(item.ID) > 36 || len(item.Label) > 800 {
			return false
		}
	}
	return true
}
func applicationValue(v domain.Application) (domain.Application, bool) {
	if !validID(v.ID) || !validID(v.PropertyID) || v.Revision <= 0 || len(v.Items) == 0 {
		return domain.Application{}, false
	}
	items := make([]domain.ChecklistItem, len(v.Items))
	seen := make(map[string]bool, len(items))
	decisions := 0
	for i, item := range v.Items {
		label, ok := textValue(item.Label, 200, false)
		if !ok || !validID(item.ID) || seen[item.ID] {
			return domain.Application{}, false
		}
		seen[item.ID] = true
		if item.HumanDecision {
			decisions++
			if item.Done {
				return domain.Application{}, false
			}
		}
		item.Label = label
		items[i] = item
	}
	if decisions != 1 {
		return domain.Application{}, false
	}
	v.Items = items
	return v, true
}
func rawProcedure(v domain.Procedure) bool {
	return len(v.ID) <= 36 && len(v.Title) <= 800 && len(v.Body) <= 24000
}
func procedureValue(v domain.Procedure) (domain.Procedure, bool) {
	title, ok := textValue(v.Title, 200, false)
	body, bodyOK := textValue(v.Body, 6000, true)
	if !ok || !bodyOK || !validID(v.ID) || v.Revision <= 0 {
		return domain.Procedure{}, false
	}
	v.Title, v.Body = title, body
	return v, true
}
func rawProperty(v repository.Property) bool {
	return len(v.ID) <= 36 && len(v.Name) <= 800 && len(v.Area) <= 800 && len(v.OwnerLabel) <= 800 && len(v.OccupantLabel) <= 800 && len(v.Occupancy) <= 8 && len(v.InspectionDate) <= 10
}
func propertyValue(v repository.Property) (repository.Property, bool) {
	if !validID(v.ID) {
		return repository.Property{}, false
	}
	for _, p := range []*string{&v.Name, &v.Area, &v.OwnerLabel, &v.OccupantLabel} {
		s, ok := textValue(*p, 200, false)
		if !ok {
			return repository.Property{}, false
		}
		*p = s
	}
	if v.Occupancy != "occupied" && v.Occupancy != "vacant" && v.Occupancy != "unknown" {
		return repository.Property{}, false
	}
	if v.InspectionDate != "" {
		d, e := time.Parse("2006-01-02", v.InspectionDate)
		if e != nil || d.Format("2006-01-02") != v.InspectionDate {
			return repository.Property{}, false
		}
	}
	return v, true
}
func rawActivity(v repository.Activity) bool {
	return len(v.ID) <= 36 && len(v.ActorID) <= 36 && len(v.ResourceID) <= 36 && len(v.Action) <= 32
}
func activityValue(v repository.Activity) (repository.Activity, bool) {
	if !validID(v.ID) || !validID(v.ActorID) || !validID(v.ResourceID) || v.Revision <= 0 || v.At.IsZero() {
		return repository.Activity{}, false
	}
	switch v.Action {
	case "case.changed", "checklist.changed", "procedure.changed", "draft.saved":
	default:
		return repository.Activity{}, false
	}
	v.At = v.At.UTC()
	return v, true
}
func tokenOK(s string) bool {
	if len(s) != 43 {
		return false
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(s)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s
}
func rawDraft(v domain.Draft) bool {
	return len(v.CaseID) <= 36 && len(v.Body) <= 16000 && rawIDs(v.SourceIDs)
}
func draftValue(v domain.Draft, c domain.Case) (domain.Draft, bool) {
	if !rawDraft(v) {
		return domain.Draft{}, false
	}
	body, ok := textValue(v.Body, 4000, true)
	if !ok || v.CaseID != c.ID || v.CaseRevision <= 0 || v.CaseRevision > c.Revision || !validIDs(v.SourceIDs) {
		return domain.Draft{}, false
	}
	v.Body = body
	v.SourceIDs = append([]string(nil), v.SourceIDs...)
	return v, true
}

func rawList[T any](p PageCursor, rows []T, raw func(T) bool) bool {
	if len(p.After) > 36 || p.Limit < 1 || p.Limit > 100 || len(rows) > p.Limit {
		return false
	}
	for _, row := range rows {
		if !raw(row) {
			return false
		}
	}
	return true
}
func listValues[T any](p PageCursor, rows []T, raw func(T) bool, normalize func(T) (T, bool), id func(T) string) ([]T, bool) {
	if !rawList(p, rows, raw) || (p.After != "" && !validID(p.After)) {
		return nil, false
	}
	out := make([]T, len(rows))
	previous := p.After
	for i, row := range rows {
		value, ok := normalize(row)
		if !ok || id(value) <= previous {
			return nil, false
		}
		out[i] = value
		previous = id(value)
	}
	return out, true
}
