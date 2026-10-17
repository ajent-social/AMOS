package payload

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ajent-social/amos/internal/diagnosticcontract"
)

func fixture(t testing.TB) []byte {
	t.Helper()
	data, err := json.Marshal(diagnosticcontract.Diagnostic{
		Schema: diagnosticcontract.Profile, Registry: diagnosticcontract.Registry, Window: "24h",
		Components: []diagnosticcontract.Component{{ID: diagnosticcontract.ComponentID, PublicBaseline: diagnosticcontract.PublicBaselineID, SourceManifestSHA256: diagnosticcontract.SourceManifestSHA256}},
		Metrics:    []diagnosticcontract.Metric{{Name: "http.requests", Bucket: "1"}},
		Errors:     []diagnosticcontract.ErrorAggregate{{Code: "handler.panic", Signature: "http.handler_panic.v1", Bucket: "1", Frames: []diagnosticcontract.Frame{{ID: "amos.health.serve_http.v1"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func metadata(data []byte) Metadata {
	return Metadata{Headers: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(data))}
}
func compress(t testing.TB, data []byte, extra string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if extra != "" {
		writer.Extra = []byte(extra)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func reject(t testing.TB, meta Metadata, data []byte) {
	t.Helper()
	got, err := Decode(meta, data)
	if !errors.Is(err, ErrInvalid) || err.Error() != "diagnostic payload rejected" || len(got.Bytes()) != 0 {
		t.Fatalf("rejection failed: %v", err)
	}
}

func TestPayloadCanonicalFreshCopies(t *testing.T) {
	original := fixture(t)
	variants := [][]byte{original, append([]byte(" \n"), original...), bytes.Replace(original, []byte(`"schema":"amos`), []byte(`"schema":"\u0061mos`), 1)}
	var reordered map[string]any
	if err := json.Unmarshal(original, &reordered); err != nil {
		t.Fatal(err)
	}
	other, err := json.MarshalIndent(reordered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	variants = append(variants, other)
	for index, data := range variants {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			data = bytes.Clone(data)
			meta := metadata(data)
			got, e := Decode(meta, data)
			if e != nil || !bytes.Equal(got.Bytes(), original) {
				t.Fatalf("canonical mismatch: %v", e)
			}
			data[0] = '!'
			meta.Headers["Content-Type"][0] = "changed"
			if !bytes.Equal(got.Bytes(), original) {
				t.Fatal("output retains caller bytes")
			}
			exposed := got.Bytes()
			exposed[0] = '!'
			if !bytes.Equal(got.Bytes(), original) {
				t.Fatal("output exposes mutable backing bytes")
			}
		})
	}
	if len((Canonical{}).Bytes()) != 0 {
		t.Fatal("zero value must be empty")
	}
}

func TestPayloadHeaderRejections(t *testing.T) {
	data := fixture(t)
	tests := map[string]func(*Metadata){
		"missing type":        func(m *Metadata) { m.Headers = nil },
		"duplicate type":      func(m *Metadata) { m.Headers["Content-Type"] = []string{"application/json", "application/json"} },
		"alias type":          func(m *Metadata) { m.Headers["content-type"] = []string{"application/json"} },
		"empty type slice":    func(m *Metadata) { m.Headers["Content-Type"] = nil },
		"combined type":       func(m *Metadata) { m.Headers.Set("Content-Type", "application/json, application/json") },
		"bad charset":         func(m *Metadata) { m.Headers.Set("Content-Type", "application/json;charset=latin1") },
		"unknown parameter":   func(m *Metadata) { m.Headers.Set("Content-Type", "application/json;x=synthetic") },
		"duplicate parameter": func(m *Metadata) { m.Headers.Set("Content-Type", "application/json;charset=utf-8;CHARSET=utf-8") },
		"empty parameter":     func(m *Metadata) { m.Headers.Set("Content-Type", "application/json;") },
		"unicode media space": func(m *Metadata) { m.Headers.Set("Content-Type", "\u00a0application/json") },
		"combined encoding":   func(m *Metadata) { m.Headers["Content-Encoding"] = []string{"identity, gzip"} },
		"duplicate encoding":  func(m *Metadata) { m.Headers["Content-Encoding"] = []string{"identity", "identity"} },
		"alias encoding": func(m *Metadata) {
			m.Headers["Content-Encoding"] = []string{"identity"}
			m.Headers["content-encoding"] = []string{"identity"}
		},
		"empty encoding":         func(m *Metadata) { m.Headers["Content-Encoding"] = []string{""} },
		"br encoding":            func(m *Metadata) { m.Headers.Set("Content-Encoding", "br") },
		"unicode encoding space": func(m *Metadata) { m.Headers.Set("Content-Encoding", "\u00a0identity") },
		"duplicate length": func(m *Metadata) {
			m.Headers["Content-Length"] = []string{strconv.Itoa(len(data)), strconv.Itoa(len(data))}
		},
		"alias length": func(m *Metadata) {
			m.Headers["Content-Length"] = []string{strconv.Itoa(len(data))}
			m.Headers["content-length"] = []string{strconv.Itoa(len(data))}
		},
		"combined length":        func(m *Metadata) { m.Headers.Set("Content-Length", "1,1") },
		"signed length":          func(m *Metadata) { m.Headers.Set("Content-Length", "+"+strconv.Itoa(len(data))) },
		"overflow length":        func(m *Metadata) { m.Headers.Set("Content-Length", "18446744073709551616") },
		"header length mismatch": func(m *Metadata) { m.Headers.Set("Content-Length", "1") },
		"unknown length":         func(m *Metadata) { m.ContentLength = -1 },
		"byte length mismatch":   func(m *Metadata) { m.ContentLength++ },
		"transfer field":         func(m *Metadata) { m.TransferEncoding = []string{"chunked"} },
		"transfer header":        func(m *Metadata) { m.Headers["transfer-encoding"] = []string{"chunked"} },
		"trailer field":          func(m *Metadata) { m.Trailers = http.Header{"X-Trailer": {"value"}} },
		"trailer header":         func(m *Metadata) { m.Headers["trailer"] = []string{"X-Trailer"} },
		"bad header name":        func(m *Metadata) { m.Headers["X Bad"] = []string{"value"} },
		"header injection":       func(m *Metadata) { m.Headers["X-Other"] = []string{"value\r\nInjected: canary"} },
		"header controls":        func(m *Metadata) { m.Headers["X-Other"] = []string{"\x00"} },
		"empty unrelated slice":  func(m *Metadata) { m.Headers["X-Other"] = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) { m := metadata(data); mutate(&m); reject(t, m, data) })
	}
	for _, value := range []string{"application/json", "APPLICATION/JSON; Charset=\"UTF-8\"", "application/json; charset=utf-8"} {
		m := metadata(data)
		m.Headers.Set("Content-Type", value)
		m.Headers.Set("Content-Encoding", " Identity ")
		m.Headers.Set("Content-Length", strconv.Itoa(len(data)))
		if _, err := Decode(m, data); err != nil {
			t.Fatalf("valid envelope rejected: %v", err)
		}
	}
}

func TestPayloadExactBounds(t *testing.T) {
	data := fixture(t)
	padded := append(bytes.Clone(data), bytes.Repeat([]byte(" "), MaxBytes-len(data))...)
	if _, err := Decode(metadata(padded), padded); err != nil {
		t.Fatal("exact identity bound rejected")
	}
	oversized := append(bytes.Clone(padded), ' ')
	reject(t, metadata(oversized), oversized)
	for _, plain := range [][]byte{padded, oversized} {
		wire := compress(t, plain, "")
		m := metadata(wire)
		m.Headers.Set("Content-Encoding", "gzip")
		_, err := Decode(m, wire)
		if (err == nil) != (len(plain) == MaxBytes) {
			t.Fatalf("decoded bound decision: %v", err)
		}
	}
	base := compress(t, data, "")
	wire := compress(t, data, strings.Repeat("x", MaxBytes-len(base)-2))
	if len(wire) != MaxBytes {
		t.Fatal("fixture does not hit exact compressed bound")
	}
	m := metadata(wire)
	m.Headers.Set("Content-Encoding", "gzip")
	if _, err := Decode(m, wire); err != nil {
		t.Fatal("exact compressed bound rejected")
	}
	wire = append(wire, 0)
	m = metadata(wire)
	m.Headers.Set("Content-Encoding", "gzip")
	reject(t, m, wire)
	m = metadata(data)
	m.Headers["X-Fill"] = []string{strings.Repeat("x", MaxHeaderBytes-42)}
	if _, err := Decode(m, data); err != nil {
		t.Fatalf("exact header bound rejected: %v", err)
	}
	m.Headers["X-Fill"][0] += "x"
	reject(t, m, data)
	// Repeated names must be charged for every actual field value.
	m = metadata(data)
	m.Headers["X-Fill"] = []string{strings.Repeat("x", MaxHeaderBytes-42), ""}
	reject(t, m, data)
	reject(t, metadata(nil), nil)
}

func TestPayloadGzipRejectTrailing(t *testing.T) {
	data := fixture(t)
	wire := compress(t, data, "")
	for name, suffix := range map[string][]byte{"garbage": {0}, "empty second member": compress(t, nil, ""), "second report": compress(t, data, "")} {
		t.Run(name, func(t *testing.T) {
			joined := append(bytes.Clone(wire), suffix...)
			m := metadata(joined)
			m.Headers.Set("Content-Encoding", "gzip")
			reject(t, m, joined)
		})
	}
}
func TestPayloadGzipIntegrity(t *testing.T) {
	wire := compress(t, fixture(t), "")
	corrupt := bytes.Clone(wire)
	corrupt[len(corrupt)-8] ^= 1
	for _, raw := range [][]byte{wire[:5], wire[:len(wire)-1], corrupt, []byte("not gzip")} {
		m := metadata(raw)
		m.Headers.Set("Content-Encoding", "gzip")
		reject(t, m, raw)
	}
}
func TestPayloadProfileRejectsRawData(t *testing.T) {
	data := fixture(t)
	for name, raw := range map[string][]byte{
		"duplicate decoded key": append([]byte(`{"schema":"attacker",`), data[1:]...),
		"escaped duplicate":     append([]byte(`{"\u0073chema":"attacker",`), data[1:]...),
		"unknown key":           append([]byte(`{"secret":"synthetic-canary",`), data[1:]...),
		"case alias":            bytes.Replace(data, []byte(`"schema"`), []byte(`"Schema"`), 1),
		"instruction signature": bytes.Replace(data, []byte("http.handler_panic.v1"), []byte("https://untrusted.invalid/run"), 1),
		"unsupported baseline":  bytes.Replace(data, []byte(diagnosticcontract.PublicBaselineID), []byte("unsupported"), 1),
		"invalid utf8":          append([]byte{0xff}, data...),
		"unpaired surrogate":    bytes.Replace(data, []byte(`"24h"`), []byte(`"\ud800"`), 1),
		"trailing value":        append(bytes.Clone(data), []byte(`{}`)...),
		"deep nesting":          []byte(strings.Repeat("[", 1000) + "0" + strings.Repeat("]", 1000)),
	} {
		t.Run(name, func(t *testing.T) { reject(t, metadata(raw), raw) })
	}
}
