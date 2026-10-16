// Package diagnosticcontract checks the finite, design-only public diagnostic
// profile. It does not sanitize local events, grant consent, export or send data.
// Its decoder is a profile checker, not a generic JSON Schema implementation.
package diagnosticcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	Profile                     = "amos-public-diagnostic-v1"
	Registry                    = "amos-public-diagnostic-registry-v1"
	SourceRevision              = "0208b87c7bc24d058d327a08d5634c6ef77af4ee"
	MaxPayloadBytes             = 8192
	MaxReportsPerRolling24Hours = 1
	MaxQueuedReports            = 4
	MaxRetentionHours           = 168
)

var ErrInvalid = errors.New("public diagnostic profile rejected")

type Diagnostic struct {
	Schema     string           `json:"schema"`
	Registry   string           `json:"registry"`
	Window     string           `json:"window"`
	Components []Component      `json:"components"`
	Metrics    []Metric         `json:"metrics"`
	Errors     []ErrorAggregate `json:"errors"`
}
type Component struct {
	ID             string `json:"id"`
	SourceRevision string `json:"source_revision"`
}
type Metric struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
}
type ErrorAggregate struct {
	Code      string  `json:"code"`
	Signature string  `json:"signature"`
	Bucket    string  `json:"bucket"`
	Frames    []Frame `json:"frames"`
}
type Frame struct {
	ID string `json:"id"`
}

// DecodeAndValidate rejects unknown/duplicate fields and values outside this
// exact reviewed registry. It returns only a generic error, never input data.
// Schema meta-validation and independent fixture validation remain separate.
func DecodeAndValidate(data []byte) error {
	if len(data) == 0 || len(data) > MaxPayloadBytes || !utf8.Valid(data) {
		return ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	if !closedValue(tokens, 0) {
		return ErrInvalid
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var v Diagnostic
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	if v.Schema != Profile || v.Registry != Registry || v.Window != "24h" || len(v.Components) != 1 || v.Components[0] != (Component{ID: "amos.core", SourceRevision: SourceRevision}) || v.Metrics == nil || len(v.Metrics) > 3 || v.Errors == nil || len(v.Errors) > 2 {
		return ErrInvalid
	}
	previous := ""
	for _, m := range v.Metrics {
		if (m.Name != "http.requests" && m.Name != "http.server_errors" && m.Name != "readiness.failures") || m.Name <= previous || !bucket(m.Bucket) {
			return ErrInvalid
		}
		previous = m.Name
	}
	previous = ""
	for _, e := range v.Errors {
		if e.Code <= previous || !bucket(e.Bucket) || e.Frames == nil || len(e.Frames) > 1 {
			return ErrInvalid
		}
		previous = e.Code
		switch e.Code {
		case "dependency.unavailable":
			if e.Signature != "http.required_dependency_unavailable.v1" {
				return ErrInvalid
			}
		case "handler.panic":
			if e.Signature != "http.handler_panic.v1" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		for _, f := range e.Frames {
			if f.ID != "amos.health.serve_http.v1" {
				return ErrInvalid
			}
		}
	}
	return nil
}
func bucket(v string) bool {
	switch v {
	case "0", "1", "2-4", "5-15", "16-63", "64+":
		return true
	}
	return false
}

// Detect duplicate object keys before the typed decoder, at every nesting level.
// This is deliberately finite JSON syntax checking, not schema interpretation.
func closedValue(d *json.Decoder, depth int) bool {
	if depth > 12 {
		return false
	}
	t, err := d.Token()
	if err != nil {
		return false
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return false
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return false
			}
			seen[s] = true
			if !closedValue(d, depth+1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for d.More() {
			if !closedValue(d, depth+1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim(']')
	}
	return false
}
