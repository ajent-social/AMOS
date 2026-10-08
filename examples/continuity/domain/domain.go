// Package domain provides pure, application-owned continuity value rules.
// IDs select records; these functions confer no authority and perform no effects.
package domain

import (
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalid          = errors.New("invalid continuity value")
	ErrConflict         = errors.New("continuity revision conflict")
	ErrDecisionDisabled = errors.New("human decision is disabled")
)

type CaseStatus string

const (
	AwaitingOwner   CaseStatus = "awaiting_owner"
	ReadyToArrange  CaseStatus = "ready_to_arrange"
	InProgress      CaseStatus = "in_progress"
	NeedsAssessment CaseStatus = "needs_assessment"
	Completed       CaseStatus = "completed"
)

type Case struct {
	ID, PropertyID, Title string
	Status                CaseStatus
	Revision              int64
	SourceIDs             []string
}

type ChecklistItem struct {
	ID, Label           string
	Done, HumanDecision bool
}
type Application struct {
	ID, PropertyID string
	Revision       int64
	Items          []ChecklistItem
}
type Procedure struct {
	ID, Title, Body string
	Revision        int64
}
type Draft struct {
	CaseID       string
	CaseRevision int64
	Body         string
	SourceIDs    []string
}

// ChangeCase records an operator assertion, not evidence of real-world completion.
func ChangeCase(current Case, expected int64, next CaseStatus) (Case, error) {
	value, ok := validateCase(current)
	if !ok || !validStatus(next) || expected <= 0 || current.Revision == math.MaxInt64 {
		return Case{}, ErrInvalid
	}
	if expected != current.Revision {
		return Case{}, ErrConflict
	}
	value.Status = next
	value.Revision++
	return value, nil
}

// SetChecklist changes a supporting item; human decisions are always disabled.
func SetChecklist(current Application, expected int64, itemID string, done bool) (Application, error) {
	value, ok := validateApplication(current)
	if !ok || !validID(itemID) || expected <= 0 || current.Revision == math.MaxInt64 {
		return Application{}, ErrInvalid
	}
	target := -1
	for i, item := range value.Items {
		if item.ID == itemID {
			target = i
			break
		}
	}
	if target < 0 {
		return Application{}, ErrInvalid
	}
	if expected != current.Revision {
		return Application{}, ErrConflict
	}
	if value.Items[target].HumanDecision {
		return Application{}, ErrDecisionDisabled
	}
	value.Items[target].Done = done
	value.Revision++
	return value, nil
}

// EditProcedure replaces plain text; it cannot change executable policy.
func EditProcedure(current Procedure, expected int64, body string) (Procedure, error) {
	title, titleOK := text(current.Title, 200, false)
	_, oldBodyOK := text(current.Body, 6000, true)
	nextBody, bodyOK := text(body, 6000, true)
	if !validID(current.ID) || !titleOK || !oldBodyOK || !bodyOK || current.Revision <= 0 || expected <= 0 || current.Revision == math.MaxInt64 {
		return Procedure{}, ErrInvalid
	}
	if expected != current.Revision {
		return Procedure{}, ErrConflict
	}
	current.Title, current.Body = title, nextBody
	current.Revision++
	return current, nil
}

// PrepareDraft returns unsent plain text tied to the supplied case revision.
// Persistence must atomically check that revision and resolve source references.
func PrepareDraft(current Case, body string) (Draft, error) {
	value, ok := validateCase(current)
	body, bodyOK := text(body, 4000, true)
	if !ok || !bodyOK {
		return Draft{}, ErrInvalid
	}
	return Draft{CaseID: value.ID, CaseRevision: value.Revision, Body: body, SourceIDs: value.SourceIDs}, nil
}

func validID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == value
}

func validStatus(value CaseStatus) bool {
	switch value {
	case AwaitingOwner, ReadyToArrange, InProgress, NeedsAssessment, Completed:
		return true
	}
	return false
}

func text(value string, max int, body bool) (string, bool) {
	if !utf8.ValidString(value) {
		return "", false
	}
	// Check before trimming so forbidden controls cannot disappear at the edges.
	for _, r := range value {
		if body {
			if (r < 32 || r == 127) && r != '\t' && r != '\n' {
				return "", false
			}
		} else if unicode.IsControl(r) {
			return "", false
		}
	}
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	return value, count >= 1 && count <= max
}

func validateCase(value Case) (Case, bool) {
	title, ok := text(value.Title, 200, false)
	if !validID(value.ID) || !validID(value.PropertyID) || !ok || !validStatus(value.Status) || value.Revision <= 0 || len(value.SourceIDs) < 1 || len(value.SourceIDs) > 32 {
		return Case{}, false
	}
	seen := make(map[string]bool, len(value.SourceIDs))
	for _, id := range value.SourceIDs {
		if !validID(id) || seen[id] {
			return Case{}, false
		}
		seen[id] = true
	}
	value.Title = title
	value.SourceIDs = append([]string(nil), value.SourceIDs...)
	return value, true
}

func validateApplication(value Application) (Application, bool) {
	if !validID(value.ID) || !validID(value.PropertyID) || value.Revision <= 0 || len(value.Items) < 1 || len(value.Items) > 32 {
		return Application{}, false
	}
	items := make([]ChecklistItem, len(value.Items))
	seen := make(map[string]bool, len(value.Items))
	decisions := 0
	for i, item := range value.Items {
		label, ok := text(item.Label, 200, false)
		if !validID(item.ID) || seen[item.ID] || !ok {
			return Application{}, false
		}
		seen[item.ID] = true
		if item.HumanDecision {
			decisions++
			if item.Done {
				return Application{}, false
			}
		}
		item.Label = label
		items[i] = item
	}
	if decisions != 1 {
		return Application{}, false
	}
	value.Items = items
	return value, true
}
