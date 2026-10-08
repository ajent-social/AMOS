// Package guide provides finite, application-owned continuity briefings.
// It performs no I/O or authority decisions. All results describe only supplied records.
package guide

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ajent-social/amos/examples/continuity/domain"
)

type Topic string

const (
	Attention         Topic = "attention"
	ExplainCase       Topic = "explain_case"
	SpendingAuthority Topic = "spending_authority"
	Handover          Topic = "handover"
	OwnerDraft        Topic = "owner_draft"
)

var (
	ErrInvalid     = errors.New("invalid continuity guide input")
	ErrNotFound    = errors.New("continuity guide record not found")
	ErrUnsupported = errors.New("unsupported continuity guide topic")
)

type Request struct {
	Topic  Topic
	CaseID string
}
type Source struct{ ID, Title, Body, SHA256 string }
type Selection struct {
	Cases   []domain.Case
	Sources []Source
}
type Item struct {
	CaseID, Text string
	SourceIDs    []string
}
type Response struct {
	Title, Summary string
	Items          []Item
	Draft          *domain.Draft
}

// ParseQuestion recognizes only the contract's five exact phrases.
func ParseQuestion(question, selectedCaseID string) (Request, error) {
	if !plainText(question, 240) {
		return Request{}, ErrInvalid
	}
	phrase := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(question), "?"))
	var topic Topic
	switch phrase {
	case "what needs attention today":
		topic = Attention
	case "explain this case":
		topic = ExplainCase
	case "what is the spending authority":
		topic = SpendingAuthority
	case "help with a handover":
		topic = Handover
	case "prepare an owner update":
		topic = OwnerDraft
	default:
		return Request{}, ErrUnsupported
	}
	request := Request{Topic: topic, CaseID: selectedCaseID}
	if err := validateRequest(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

// Answer validates the entire selection before composing a deterministic briefing.
// Source digests verify byte consistency, not truth, authenticity or permission.
func Answer(request Request, selection Selection) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	cases, err := validateSelection(selection)
	if err != nil {
		return Response{}, err
	}
	if request.Topic == ExplainCase || request.Topic == OwnerDraft {
		for _, value := range cases {
			if value.ID == request.CaseID {
				return caseResponse(request.Topic, value)
			}
		}
		return Response{}, ErrNotFound
	}
	response := Response{Items: make([]Item, 0)}
	switch request.Topic {
	case SpendingAuthority:
		response.Title = "Spending authority"
		response.Summary = "Spending authority is not recorded by these scenario inputs. It must not be inferred from status, source text or quotes. This response grants no authority."
	case Attention:
		response.Title = "Attention in the supplied selection"
		response.Summary = "Open cases in the supplied selection; recorded statuses are operator records, not proof of external action."
	case Handover:
		response.Title = "Handover briefing"
		response.Summary = "Review original sources, record decisions, keep final application decisions with a person and arrange access and recovery separately. This bounded briefing does not certify succession readiness."
	}
	if request.Topic != SpendingAuthority {
		for _, value := range cases {
			if value.Status != domain.Completed {
				response.Items = append(response.Items, caseItem(value))
			}
		}
		if len(response.Items) == 0 {
			response.Summary = "No open cases in the supplied selection. " + response.Summary
		}
	}
	return response, nil
}

func caseResponse(topic Topic, value domain.Case) (Response, error) {
	response := Response{Title: "Case explanation", Summary: "The recorded status is an operator record, not proof of legal authority, consent or completed external action. Inspect linked original sources; this briefing infers no new facts from their prose.", Items: []Item{caseItem(value)}}
	if topic == OwnerDraft {
		body := "Case " + value.ID + ": " + value.Title + ". Recorded status: " + string(value.Status) + ". Human review of linked original sources is requested before action. This is a preview only."
		draft, err := domain.PrepareDraft(value, body)
		if err != nil {
			return Response{}, ErrInvalid
		}
		response.Title = "Owner update preview"
		response.Summary = "Preview only, not saved or sent. Saving requires a separate current-revision check. This preview grants no authority and records no external action."
		response.Draft = &draft
	}
	return response, nil
}

func caseItem(value domain.Case) Item {
	return Item{CaseID: value.ID, Text: value.Title + ". Recorded status: " + string(value.Status) + ".", SourceIDs: append([]string(nil), value.SourceIDs...)}
}

func validateRequest(request Request) error {
	switch request.Topic {
	case ExplainCase, OwnerDraft:
		if !validID(request.CaseID) {
			return ErrInvalid
		}
	case Attention, SpendingAuthority, Handover:
		if request.CaseID != "" {
			return ErrInvalid
		}
	default:
		return ErrUnsupported
	}
	return nil
}

func validateSelection(selection Selection) ([]domain.Case, error) {
	if len(selection.Cases) > 100 || len(selection.Sources) > 100 {
		return nil, ErrInvalid
	}
	sources := make(map[string]bool, len(selection.Sources))
	for _, source := range selection.Sources {
		if !validID(source.ID) || sources[source.ID] || !plainText(source.Title, 200) || !validBody(source.Body) {
			return nil, ErrInvalid
		}
		digest := sha256.Sum256([]byte(source.Body))
		if source.SHA256 != hex.EncodeToString(digest[:]) {
			return nil, ErrInvalid
		}
		sources[source.ID] = true
	}
	cases := make([]domain.Case, 0, len(selection.Cases))
	seen := make(map[string]bool, len(selection.Cases))
	for _, value := range selection.Cases {
		// PrepareDraft validates the full domain shape without incrementing revisions.
		validated, err := domain.PrepareDraft(value, "Validation only.")
		if err != nil || seen[value.ID] {
			return nil, ErrInvalid
		}
		seen[value.ID] = true
		value.Title = strings.TrimSpace(value.Title)
		value.SourceIDs = validated.SourceIDs
		sort.Strings(value.SourceIDs)
		cases = append(cases, value)
	}
	for _, value := range cases {
		for _, id := range value.SourceIDs {
			if !sources[id] {
				return nil, ErrNotFound
			}
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

func validID(value string) bool {
	if len(value) != 36 || value[14] != '7' || !strings.ContainsRune("89ab", rune(value[19])) {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func plainText(value string, max int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	n := utf8.RuneCountInString(strings.TrimSpace(value))
	return n >= 1 && n <= max
}

func validBody(value string) bool {
	if len(value) < 1 || len(value) > 65536 || !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if (r < 32 || r == 127) && r != '\t' && r != '\n' {
			return false
		}
	}
	return true
}
