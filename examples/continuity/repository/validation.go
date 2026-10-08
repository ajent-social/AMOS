package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/google/uuid"
)

func validID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == s
}
func textValue(s string, max int, body bool) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	for _, c := range s {
		if body {
			if (c < 32 || c == 127) && c != '\t' && c != '\n' {
				return "", false
			}
		} else if unicode.IsControl(c) {
			return "", false
		}
	}
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n > 0 && n <= max
}
func strictJSON(b []byte, out any) bool {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}
func caseValue(c domain.Case) (domain.Case, bool) {
	if _, err := domain.PrepareDraft(c, "validation"); err != nil {
		return domain.Case{}, false
	}
	c.Title = strings.TrimSpace(c.Title)
	return c, true
}
func appValue(a domain.Application) (domain.Application, bool) {
	if a.Revision <= 0 || len(a.Items) == 0 {
		return domain.Application{}, false
	}
	probe := a
	probe.Revision = 1
	v, err := domain.SetChecklist(probe, 1, a.Items[0].ID, a.Items[0].Done)
	if err != nil && !errors.Is(err, domain.ErrDecisionDisabled) {
		return domain.Application{}, false
	}
	if err == nil {
		a.Items = v.Items
	} else {
		for i := range a.Items {
			a.Items[i].Label = strings.TrimSpace(a.Items[i].Label)
		}
	}
	return a, true
}
func procedureValue(p domain.Procedure) (domain.Procedure, bool) {
	if p.Revision <= 0 {
		return domain.Procedure{}, false
	}
	rev := p.Revision
	p.Revision = 1
	v, err := domain.EditProcedure(p, 1, p.Body)
	if err != nil {
		return domain.Procedure{}, false
	}
	v.Revision = rev
	return v, true
}
func propertyValue(p Property) (Property, bool) {
	if !validID(p.ID) {
		return Property{}, false
	}
	for _, s := range []*string{&p.Name, &p.Area, &p.OwnerLabel, &p.OccupantLabel} {
		v, ok := textValue(*s, 200, false)
		if !ok {
			return Property{}, false
		}
		*s = v
	}
	if p.Occupancy != "occupied" && p.Occupancy != "vacant" && p.Occupancy != "unknown" {
		return Property{}, false
	}
	if p.InspectionDate != "" {
		v, e := time.Parse("2006-01-02", p.InspectionDate)
		if e != nil || v.Format("2006-01-02") != p.InspectionDate {
			return Property{}, false
		}
	}
	return p, true
}
func sourceValue(s Source) (Source, bool) {
	title, ok := textValue(s.Title, 200, false)
	if !validID(s.ID) || !ok || (s.Kind != "correspondence" && s.Kind != "document") || len(s.Body) > 65536 {
		return Source{}, false
	}
	if _, ok := textValue(s.Body, 65536, true); !ok {
		return Source{}, false
	}
	sum := sha256.Sum256([]byte(s.Body))
	if s.SHA256 != hex.EncodeToString(sum[:]) {
		return Source{}, false
	}
	s.Title = title
	return s, true
}
func domainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		return ErrInvalid
	case errors.Is(err, domain.ErrConflict):
		return ErrConflict
	case errors.Is(err, domain.ErrDecisionDisabled):
		return ErrDecisionDisabled
	default:
		return ErrUnavailable
	}
}
func pageInput(after, query string, limit int) (string, bool) {
	if limit < 1 || limit > 100 || (after != "" && !validID(after)) {
		return "", false
	}
	if query == "" {
		return "", true
	}
	return textValue(query, 120, false)
}
func searchPattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
}
func validAction(s string) bool {
	return s == "case.changed" || s == "checklist.changed" || s == "procedure.changed" || s == "draft.saved"
}

// Stored checklist objects have one exact wire shape. encoding/json alone
// accepts case-insensitive field names, missing booleans and null booleans.
func checklistJSON(raw []byte, out *[]domain.ChecklistItem) bool {
	if len(raw) > 65536 {
		return false
	}
	var objects []map[string]json.RawMessage
	if !strictJSON(raw, &objects) || len(objects) < 1 || len(objects) > 32 {
		return false
	}
	for _, o := range objects {
		if len(o) != 4 {
			return false
		}
		for _, key := range []string{"ID", "Label", "Done", "HumanDecision"} {
			if _, ok := o[key]; !ok {
				return false
			}
		}
		for _, key := range []string{"Done", "HumanDecision"} {
			if string(o[key]) != "true" && string(o[key]) != "false" {
				return false
			}
		}
	}
	return strictJSON(raw, out)
}
