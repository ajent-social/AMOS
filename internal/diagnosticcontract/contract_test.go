package diagnosticcontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestPublicDiagnosticSchema(t *testing.T) {
	valid := fixture(t, "valid.json")
	if err := DecodeAndValidate(valid); err != nil {
		t.Fatal(err)
	}
	rejected := []string{"rejected-application-frame.json", "rejected-secret-metadata.json"}
	for _, name := range rejected {
		t.Run(name, func(t *testing.T) {
			if err := DecodeAndValidate(fixture(t, name)); !errors.Is(err, ErrInvalid) {
				t.Fatal("private fixture was not rejected")
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"raw logs", func(v map[string]any) { v["logs"] = "synthetic private log" }},
		{"source", func(v map[string]any) { v["source"] = "private application source" }},
		{"unknown nested metric", func(v map[string]any) {
			v["metrics"].([]any)[0].(map[string]any)["metadata"] = map[string]any{"host": "private-node"}
		}},
		{"infrastructure identifier", func(v map[string]any) { v["components"].([]any)[0].(map[string]any)["host"] = "node.example.invalid" }},
		{"caller version", func(v map[string]any) {
			v["components"].([]any)[0].(map[string]any)["source_manifest_sha256"] = "caller-version"
		}},
		{"unapproved AMSL", func(v map[string]any) { v["components"].([]any)[0].(map[string]any)["id"] = "amsl" }},
		{"freeform error", func(v map[string]any) { v["errors"].([]any)[0].(map[string]any)["message"] = "Bearer synthetic-canary" }},
		{"mismatched signature", func(v map[string]any) { v["errors"].([]any)[0].(map[string]any)["signature"] = "http.handler_panic.v1" }},
		{"caller signature", func(v map[string]any) {
			v["errors"].([]any)[0].(map[string]any)["signature"] = "example.test/business/private"
		}},
		{"frame extras", func(v map[string]any) {
			v["errors"].([]any)[0].(map[string]any)["frames"].([]any)[0].(map[string]any)["arguments"] = "secret"
		}},
		{"unapproved frame", func(v map[string]any) {
			v["errors"].([]any)[0].(map[string]any)["frames"].([]any)[0].(map[string]any)["id"] = "business.private.v1"
		}},
		{"arbitrary metric", func(v map[string]any) { v["metrics"].([]any)[0].(map[string]any)["name"] = "customer.name" }},
		{"metric value exfiltration", func(v map[string]any) { v["metrics"].([]any)[0].(map[string]any)["bucket"] = "account-identifier" }},
		{"precise timestamp", func(v map[string]any) { v["timestamp"] = "2026-10-16T01:02:03Z" }},
		{"missing errors", func(v map[string]any) { delete(v, "errors") }},
		{"null metrics", func(v map[string]any) { v["metrics"] = nil }},
		{"count limit", func(v map[string]any) {
			v["components"] = []any{v["components"].([]any)[0], v["components"].([]any)[0]}
		}},
		{"duplicate metric name", func(v map[string]any) {
			v["metrics"] = []any{map[string]any{"name": "http.requests", "bucket": "1"}, map[string]any{"name": "http.requests", "bucket": "2-4"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			if err := json.Unmarshal(valid, &v); err != nil {
				t.Fatal(err)
			}
			tc.edit(v)
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if err := DecodeAndValidate(b); !errors.Is(err, ErrInvalid) {
				t.Fatal("out-of-profile fixture accepted")
			}
			if DecodeAndValidate(b).Error() != ErrInvalid.Error() {
				t.Fatal("diagnostic echoed rejected content")
			}
		})
	}
	for _, bad := range [][]byte{append(bytes.Clone(valid), valid...), append(bytes.Repeat([]byte(" "), MaxPayloadBytes), valid...), bytes.Replace(valid, []byte(`"window": "24h"`), []byte(`"window":"24h","window":"24h"`), 1), []byte("\xff")} {
		if !errors.Is(DecodeAndValidate(bad), ErrInvalid) {
			t.Fatal("invalid wire encoding accepted")
		}
	}
}

// This is a rejected design fixture and its expected omission, not execution of
// a sanitizer. T14.2 must implement and independently test the real projection.
func TestPublicDiagnosticSchemaPrivateFrameMustBeOmitted(t *testing.T) {
	raw := fixture(t, "rejected-application-frame.json")
	var rawDocument map[string]any
	if err := json.Unmarshal(raw, &rawDocument); err != nil {
		t.Fatal(err)
	}
	frame := rawDocument["errors"].([]any)[0].(map[string]any)["frames"].([]any)[0].(map[string]any)
	if frame["file"] != "/home"+"/example/private/account.go" {
		t.Fatal("fixture did not decode the synthetic home path")
	}
	if DecodeAndValidate(raw) == nil {
		t.Fatal("raw application frame accepted")
	}
	var expected Diagnostic
	if err := json.Unmarshal(fixture(t, "valid.json"), &expected); err != nil {
		t.Fatal(err)
	}
	expected.Errors[0].Frames = []Frame{}
	encoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeAndValidate(encoded); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"PrivateAccount", "account.go", "/home/", "symbol", "file"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatal("omitted-frame fixture retained private fields")
		}
	}
}

func TestPublicDiagnosticSchemaIsClosedAndFinite(t *testing.T) {
	data, err := os.ReadFile("../../api/schemas/upstream-diagnostic.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatal("unexpected schema dialect")
	}
	var inspect func(any)
	inspect = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if v["type"] == "object" && v["additionalProperties"] != false {
				t.Fatal("schema permits arbitrary object keys")
			}
			if v["type"] == "array" && v["maxItems"] == nil {
				t.Fatal("schema has unbounded array")
			}
			if v["type"] == "string" && v["enum"] == nil && v["const"] == nil {
				t.Fatal("schema has arbitrary string channel")
			}
			for _, child := range v {
				inspect(child)
			}
		case []any:
			for _, child := range v {
				inspect(child)
			}
		}
	}
	inspect(schema)
	for _, literal := range []string{Profile, Registry, ComponentID, PublicBaselineID, SourceManifestSHA256, "amos.health.serve_http.v1"} {
		if !strings.Contains(string(data), literal) {
			t.Fatal("schema and finite registry disagree")
		}
	}
	if MaxPayloadBytes != 8192 || MaxQueuedReports != 4 || MaxReportsPerRolling24Hours != 1 || MaxRetentionHours != 168 {
		t.Fatal("privacy budget changed without profile review")
	}
}

// The approved source manifest is immutable registry data. Repository evolution
// is allowed without auto-admitting changed code or requiring diagnostic export.
// Actual source/build provenance comparisons are separate qualification checks.
func TestPublicDiagnosticSchemaSourceManifest(t *testing.T) {
	digest := sha256.Sum256([]byte(PublicSourceManifest))
	if hex.EncodeToString(digest[:]) != SourceManifestSHA256 {
		t.Fatal("component manifest digest differs")
	}
	paths := []string{"app/app.go", "internal/health/handler.go", "internal/runtime/runtime.go", "internal/telemetry/logging.go"}
	lines := strings.Split(strings.TrimSuffix(PublicSourceManifest, "\n"), "\n")
	if len(lines) != len(paths) {
		t.Fatal("approved component file inventory changed")
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != paths[i] {
			t.Fatal("invalid approved component manifest entry")
		}
		digest, err := hex.DecodeString(fields[1])
		if err != nil || len(digest) != sha256.Size || strings.ToLower(fields[1]) != fields[1] {
			t.Fatal("invalid approved component source digest")
		}
	}
}

// JSON Schema's uniqueItems compares whole objects. Canonical aggregate key
// uniqueness/order are additional finite-profile requirements, never claimed
// as implications of generic schema validation.
func TestPublicDiagnosticSchemaCanonicalProfileIsStricter(t *testing.T) {
	for _, metrics := range [][]Metric{
		{{Name: "http.requests", Bucket: "1"}, {Name: "http.requests", Bucket: "2-4"}},
		{{Name: "readiness.failures", Bucket: "1"}, {Name: "http.requests", Bucket: "1"}},
	} {
		var d Diagnostic
		if err := json.Unmarshal(fixture(t, "valid.json"), &d); err != nil {
			t.Fatal(err)
		}
		d.Metrics = metrics
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if DecodeAndValidate(b) == nil {
			t.Fatal("noncanonical profile accepted")
		}
	}
}

func TestPublicDiagnosticSchemaRejectsCaseAliases(t *testing.T) {
	valid := fixture(t, "valid.json")
	for _, tc := range []struct{ name, key, alias, value string }{
		{"root", "schema", "Schema", "amos-public-diagnostic-v1"},
		{"component", "source_manifest_sha256", "Source_Manifest_SHA256", SourceManifestSHA256},
		{"metric", "name", "Name", "http.requests"},
		{"error", "signature", "Signature", "http.required_dependency_unavailable.v1"},
		{"frame", "id", "ID", "amos.health.serve_http.v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := []byte(`"` + tc.key + `": "` + tc.value + `"`)
			if bytes.Count(valid, original) != 1 {
				t.Fatal("ambiguous regression fixture")
			}
			for _, replacement := range []string{
				`"` + tc.alias + `": "` + tc.value + `"`,
				`"` + tc.alias + `": "CANARY_SECRET", ` + string(original),
				string(original) + `, "` + tc.alias + `": "` + tc.value + `"`,
			} {
				bad := bytes.Replace(valid, original, []byte(replacement), 1)
				if !errors.Is(DecodeAndValidate(bad), ErrInvalid) {
					t.Fatal("case alias or overwritten unknown canary accepted")
				}
			}
		})
	}
	for _, replacement := range []string{
		`"\u0053chema":"CANARY_SECRET", "schema":`,
		`"\u0073chema":"CANARY_SECRET", "schema":`,
		`"\u0000schema":"CANARY_SECRET", "schema":`,
		`"metaDATA":"CANARY_SECRET", "schema":`,
	} {
		bad := bytes.Replace(valid, []byte(`"schema":`), []byte(replacement), 1)
		if !errors.Is(DecodeAndValidate(bad), ErrInvalid) {
			t.Fatal("escaped alias, control key or mixed-case unknown accepted")
		}
	}
	// JSON escapes decode to actual member names. An escaped exact lowercase key
	// is still the same permitted schema property, not a case alias.
	exact := bytes.Replace(valid, []byte(`"schema":`), []byte(`"\u0073chema":`), 1)
	if err := DecodeAndValidate(exact); err != nil {
		t.Fatal("exact decoded lowercase member rejected", err)
	}
}
