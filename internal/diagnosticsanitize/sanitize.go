// Package diagnosticsanitize constructs bounded local diagnostic previews from
// untrusted structured events. It performs no collection, storage or transmission.
// A validated preview is not build provenance, consent or export authorization.
package diagnosticsanitize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/ajent-social/amos/internal/diagnosticcontract"
)

const (
	MaxInputBytes   = 65536
	MaxDepth        = 16
	MaxNodes        = 4096
	MaxArrayItems   = 256
	MaxObjectFields = 64
	MaxStringBytes  = 4096
	LocalOnly       = "unqualified-local-preview"
)

var ErrInvalid = errors.New("local diagnostic projection unavailable")

// Receipt proves only that these preview bytes passed the finite wire checker.
// It cannot attest the executing build, truthful aggregates or export eligibility.
type Receipt struct{ Profile, Registry, PayloadSHA256, Qualification string }

// Preview owns fresh bytes; callers receive copies and cannot alter its receipt.
// There is deliberately no export method, provenance callback or trust flag.
type Preview struct {
	payload []byte
	receipt Receipt
}

func (p Preview) Bytes() []byte    { return bytes.Clone(p.payload) }
func (p Preview) Receipt() Receipt { return p.receipt }

// Construct projects only exact approved metric/code names and numeric counts.
// Unknown fields (including versions, raw messages, labels and metadata) are
// never copied. Counts are unsigned JSON integers; sums saturate at 64 before
// arithmetic. Input aliases/encodings are not interpreted as approved names.
// Every input frame is omitted: no qualified executing-build provenance adapter
// exists. Even a matching public symbol/hash or caller boolean is not evidence.
func Construct(input []byte) (Preview, error) {
	if len(input) == 0 || len(input) > MaxInputBytes || !utf8.Valid(input) {
		return Preview{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.UseNumber()
	b := budget{}
	value, err := b.value(d, 0)
	if err != nil {
		return Preview{}, ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return Preview{}, ErrInvalid
	}
	root, ok := value.(map[string]any)
	if !ok {
		return Preview{}, ErrInvalid
	}
	metrics := map[string]uint8{}
	errorsByCode := map[string]uint8{}
	if raw, present := root["metrics"]; present {
		items, ok := raw.([]any)
		if !ok {
			return Preview{}, ErrInvalid
		}
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				return Preview{}, ErrInvalid
			}
			name, _ := fields["name"].(string)
			if name != "http.requests" && name != "http.server_errors" && name != "readiness.failures" {
				continue
			}
			count, err := count(fields["count"])
			if err != nil {
				return Preview{}, ErrInvalid
			}
			metrics[name] = add(metrics[name], count)
		}
	}
	if raw, present := root["errors"]; present {
		items, ok := raw.([]any)
		if !ok {
			return Preview{}, ErrInvalid
		}
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				return Preview{}, ErrInvalid
			}
			code, _ := fields["code"].(string)
			if code != "dependency.unavailable" && code != "handler.panic" {
				continue
			}
			count, err := count(fields["count"])
			if err != nil {
				return Preview{}, ErrInvalid
			}
			errorsByCode[code] = add(errorsByCode[code], count)
		}
	}
	// Constants describe the target candidate vocabulary only. Selecting them
	// here does not claim the running component matches the approved source.
	out := diagnosticcontract.Diagnostic{
		Schema: diagnosticcontract.Profile, Registry: diagnosticcontract.Registry, Window: "24h",
		Components: []diagnosticcontract.Component{{ID: diagnosticcontract.ComponentID, PublicBaseline: diagnosticcontract.PublicBaselineID, SourceManifestSHA256: diagnosticcontract.SourceManifestSHA256}},
		Metrics:    []diagnosticcontract.Metric{}, Errors: []diagnosticcontract.ErrorAggregate{},
	}
	for _, name := range keys(metrics) {
		out.Metrics = append(out.Metrics, diagnosticcontract.Metric{Name: name, Bucket: bucket(metrics[name])})
	}
	for _, code := range keys(errorsByCode) {
		signature := "http.required_dependency_unavailable.v1"
		if code == "handler.panic" {
			signature = "http.handler_panic.v1"
		}
		out.Errors = append(out.Errors, diagnosticcontract.ErrorAggregate{Code: code, Signature: signature, Bucket: bucket(errorsByCode[code]), Frames: []diagnosticcontract.Frame{}})
	}
	payload, err := json.Marshal(out)
	if err != nil || diagnosticcontract.DecodeAndValidate(payload) != nil {
		return Preview{}, ErrInvalid
	}
	digest := sha256.Sum256(payload)
	return Preview{payload: payload, receipt: Receipt{Profile: diagnosticcontract.Profile, Registry: diagnosticcontract.Registry, PayloadSHA256: hex.EncodeToString(digest[:]), Qualification: LocalOnly}}, nil
}
func keys(values map[string]uint8) []string {
	r := make([]string, 0, len(values))
	for k := range values {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func count(value any) (uint8, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, ErrInvalid
	}
	n, err := strconv.ParseUint(string(number), 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	if n >= 64 {
		return 64, nil
	}
	return uint8(n), nil
}
func add(a, b uint8) uint8 {
	if a >= 64 || b >= 64 || a >= 64-b {
		return 64
	}
	return a + b
}
func bucket(n uint8) string {
	switch {
	case n == 0:
		return "0"
	case n == 1:
		return "1"
	case n <= 4:
		return "2-4"
	case n <= 15:
		return "5-15"
	case n <= 63:
		return "16-63"
	default:
		return "64+"
	}
}

// The bounded local parser allows unknown data to be discarded by projection,
// but never permits duplicate decoded keys, unbounded recursion or huge arrays.
type budget struct{ nodes int }

func (b *budget) token(d *json.Decoder) (json.Token, error) {
	b.nodes++
	if b.nodes > MaxNodes {
		return nil, ErrInvalid
	}
	t, err := d.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	if s, ok := t.(string); ok && len(s) > MaxStringBytes {
		return nil, ErrInvalid
	}
	return t, nil
}
func (b *budget) value(d *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, ErrInvalid
	}
	t, err := b.token(d)
	if err != nil {
		return nil, err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for d.More() {
			if len(object) >= MaxObjectFields {
				return nil, ErrInvalid
			}
			key, err := b.token(d)
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, ErrInvalid
			}
			if _, exists := object[name]; exists {
				return nil, ErrInvalid
			}
			value, err := b.value(d, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		end, err := b.token(d)
		if err != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return object, nil
	case '[':
		array := []any{}
		for d.More() {
			if len(array) >= MaxArrayItems {
				return nil, ErrInvalid
			}
			v, err := b.value(d, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, v)
		}
		end, err := b.token(d)
		if err != nil || end != json.Delim(']') {
			return nil, ErrInvalid
		}
		return array, nil
	}
	return nil, ErrInvalid
}
