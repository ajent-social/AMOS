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
	Profile              = "amos-public-diagnostic-v1"
	Registry             = "amos-public-diagnostic-registry-v1"
	ComponentID          = "amos.http-observability"
	PublicBaselineID     = "amos.http-observability.v1"
	SourceManifestSHA256 = "40020920667a6ff56650b4d5e792356ca6f522b3a0e5c729f68074a6053dbdd3"
	// PublicSourceManifest is sorted UTF-8 path, space, SHA-256, newline.
	// Registry/schema/docs are excluded, so approval does not hash itself.
	PublicSourceManifest = `app/app.go 37d292c2fe6578c1030447e4b948fd34b162c622ace3cf53b9545de40bc08a77
internal/health/handler.go 87b0a138f70e12ba67026ab5150d48083fb6027e6346b067c90c7d45b8d63e0b
internal/runtime/runtime.go c104667bad38fe1dd8ee8e6da0927837152935f5a7fc8d2f40ad38012e31f53f
internal/telemetry/logging.go c1adf2275d6ca681c958c8a39be9ff5353e70292330d7a693275444922480f67
`
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
	ID                   string `json:"id"`
	PublicBaseline       string `json:"public_baseline"`
	SourceManifestSHA256 string `json:"source_manifest_sha256"`
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
	if !exactValue(tokens, rootObject) {
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
	if v.Schema != Profile || v.Registry != Registry || v.Window != "24h" || len(v.Components) != 1 || v.Components[0] != (Component{ID: ComponentID, PublicBaseline: PublicBaselineID, SourceManifestSHA256: SourceManifestSHA256}) || v.Metrics == nil || len(v.Metrics) > 3 || v.Errors == nil || len(v.Errors) > 2 {
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

// Every JSON object is checked against exact decoded property names before
// encoding/json reaches the typed structs: its case-insensitive field matching
// must never turn an unknown secret-bearing alias into an ignored overwrite.
// This fixed profile grammar is not a general JSON Schema interpreter.
type wireNode uint8

const (
	rootObject wireNode = iota
	componentObject
	metricObject
	errorObject
	frameObject
	stringValue
	componentsArray
	metricsArray
	errorsArray
	framesArray
)

func fieldNode(object wireNode, key string) (wireNode, bool) {
	switch object {
	case rootObject:
		switch key {
		case "schema", "registry", "window":
			return stringValue, true
		case "components":
			return componentsArray, true
		case "metrics":
			return metricsArray, true
		case "errors":
			return errorsArray, true
		}
	case componentObject:
		switch key {
		case "id", "public_baseline", "source_manifest_sha256":
			return stringValue, true
		}
	case metricObject:
		switch key {
		case "name", "bucket":
			return stringValue, true
		}
	case errorObject:
		switch key {
		case "code", "signature", "bucket":
			return stringValue, true
		case "frames":
			return framesArray, true
		}
	case frameObject:
		if key == "id" {
			return stringValue, true
		}
	}
	return 0, false
}

func exactValue(d *json.Decoder, node wireNode) bool {
	token, err := d.Token()
	if err != nil {
		return false
	}
	if node == stringValue {
		_, ok := token.(string)
		return ok
	}
	var element wireNode
	switch node {
	case componentsArray:
		element = componentObject
	case metricsArray:
		element = metricObject
	case errorsArray:
		element = errorObject
	case framesArray:
		element = frameObject
	default:
		if token != json.Delim('{') {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return false
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return false
			}
			child, allowed := fieldNode(node, name)
			if !allowed {
				return false
			}
			seen[name] = true
			if !exactValue(d, child) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim('}')
	}
	if token != json.Delim('[') {
		return false
	}
	for d.More() {
		if !exactValue(d, element) {
			return false
		}
	}
	end, err := d.Token()
	return err == nil && end == json.Delim(']')
}
