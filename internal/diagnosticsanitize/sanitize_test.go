package diagnosticsanitize

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/ajent-social/amos/internal/diagnosticcontract"
)

func inspect(t testing.TB, p Preview) diagnosticcontract.Diagnostic {
	t.Helper()
	payload := p.Bytes()
	if err := diagnosticcontract.DecodeAndValidate(payload); err != nil {
		t.Fatal("invalid preview", err)
	}
	var d diagnosticcontract.Diagnostic
	if err := json.Unmarshal(payload, &d); err != nil {
		t.Fatal(err)
	}
	for _, e := range d.Errors {
		if len(e.Frames) != 0 {
			t.Fatal("unqualified frame admitted")
		}
	}
	hash := sha256.Sum256(payload)
	r := p.Receipt()
	if r.Qualification != LocalOnly || r.PayloadSHA256 != hex.EncodeToString(hash[:]) || r.Profile != diagnosticcontract.Profile || r.Registry != diagnosticcontract.Registry {
		t.Fatal("preview receipt is not local-only or hash bound")
	}
	return d
}
func transport(t testing.TB, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "" || r.Comment != "" || !r.ModTime.IsZero() {
		t.Fatal("compressed fixture contains hidden header metadata")
	}
	decoded, err := io.ReadAll(io.LimitReader(r, diagnosticcontract.MaxPayloadBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatal("compressed fixture changed preview")
	}
	return out.Bytes()
}
func TestSanitizationFreshProjectionAndCompressedPrivacy(t *testing.T) {
	const canary = "synthetic-diagnostic-private-canary"
	encoded := base64.StdEncoding.EncodeToString([]byte(canary))
	input := map[string]any{
		"source_revision": "caller-release", "approved": true, "source_manifest_sha256": diagnosticcontract.SourceManifestSHA256,
		"raw_message": encoded, "metadata": map[string]any{"nested": []any{map[string]any{"token": canary}}},
		"metrics": []any{map[string]any{"name": "http.requests", "count": 3, "labels": map[string]any{"secret": canary}}, map[string]any{"name": canary, "count": 1}},
		"errors": []any{map[string]any{"code": "handler.panic", "count": 1, "message": "https://service.example.invalid/path?token=" + canary, "frames": []any{
			map[string]any{"symbol": "example.test/business.PrivateAccount", "file": "/home" + "/example/private.go", "arguments": []string{canary}},
			map[string]any{"symbol": "github.com/ajent-social/amos/internal/health.(*Handler).ServeHTTP", "source_manifest_sha256": diagnosticcontract.SourceManifestSHA256, "approved": true},
			map[string]any{"id": "amos.health.serve_http.v1", "symbol": "example.test/business.PrivateAccount", "arguments": []string{canary}, "address": "0x1234"},
		}}},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Construct(raw)
	if err != nil {
		t.Fatal(err)
	}
	d := inspect(t, p)
	if len(d.Metrics) != 1 || d.Metrics[0].Name != "http.requests" || d.Metrics[0].Bucket != "2-4" || len(d.Errors) != 1 || d.Errors[0].Code != "handler.panic" {
		t.Fatal("approved aggregates lost")
	}
	clean, err := Construct([]byte(`{"metrics":[{"name":"http.requests","count":3}],"errors":[{"code":"handler.panic","count":1}]}`))
	if err != nil || !bytes.Equal(clean.Bytes(), p.Bytes()) || clean.Receipt() != p.Receipt() {
		t.Fatal("private unknown fields influenced the public projection")
	}
	compressed := transport(t, p.Bytes())
	for _, stream := range [][]byte{p.Bytes(), compressed} {
		for _, private := range []string{canary, encoded, "PrivateAccount", "private.go", "/home/", "caller-release", "raw_message", "metadata", "arguments", "source_revision", "0x1234", "service.example.invalid"} {
			if bytes.Contains(stream, []byte(private)) {
				t.Fatalf("private field leaked: %q", private)
			}
		}
	}
	// Copies and caller mutations cannot rewrite the immutable hash receipt.
	snapshot := p.Bytes()
	snapshot[0] = '!'
	if p.Bytes()[0] != '{' {
		t.Fatal("preview aliases caller bytes")
	}
	for i := range raw {
		raw[i] = '!'
	}
	inspect(t, p)
	receipt := p.Receipt()
	receipt.Qualification = "export-approved"
	if p.Receipt().Qualification != LocalOnly {
		t.Fatal("receipt was mutable")
	}
}
func TestSanitizationCanonicalCountsAndOverflow(t *testing.T) {
	a := []byte(`{"metrics":[{"name":"readiness.failures","count":1},{"name":"http.requests","count":18446744073709551615},{"name":"http.requests","count":18446744073709551615},{"name":"http.server_errors","count":5}],"errors":[{"code":"handler.panic","count":4},{"code":"dependency.unavailable","count":0}]}`)
	b := []byte(`{"errors":[{"count":0,"code":"dependency.unavailable"},{"count":4,"code":"handler.panic"}],"metrics":[{"name":"http.requests","count":18446744073709551615},{"name":"http.server_errors","count":5},{"name":"http.requests","count":18446744073709551615},{"name":"readiness.failures","count":1}]}`)
	p, err := Construct(a)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Construct(b)
	if err != nil {
		t.Fatal(err)
	}
	d := inspect(t, p)
	inspect(t, q)
	if !bytes.Equal(p.Bytes(), q.Bytes()) || p.Receipt() != q.Receipt() {
		t.Fatal("equivalent input order changed canonical bytes/hash")
	}
	if d.Metrics[0].Bucket != "64+" || d.Metrics[1].Bucket != "5-15" || d.Errors[0].Bucket != "0" || d.Errors[1].Bucket != "2-4" {
		t.Fatal("aggregation overflow or bucket changed")
	}
	for _, tc := range []struct{ count, bucket string }{{"0", "0"}, {"1", "1"}, {"2", "2-4"}, {"4", "2-4"}, {"5", "5-15"}, {"15", "5-15"}, {"16", "16-63"}, {"63", "16-63"}, {"64", "64+"}, {"18446744073709551615", "64+"}} {
		p, err := Construct([]byte(`{"metrics":[{"name":"http.requests","count":` + tc.count + `}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if inspect(t, p).Metrics[0].Bucket != tc.bucket {
			t.Fatal("wrong boundary bucket", tc.count)
		}
	}
	for _, bad := range []string{"-1", "1.5", "1e2", "18446744073709551616", `"3"`, "null", "true"} {
		if _, err := Construct([]byte(`{"metrics":[{"name":"http.requests","count":` + bad + `}]}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid count accepted", bad)
		}
	}
}
func TestSanitizationBoundedEncodedAndUnknownCorpus(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"Metrics":[{"name":"http.requests","count":1}]}`, `{"metrics":[{"name":"http.requests\nsecret","count":1},{"name":"http%2erequests","count":2},{"name":"aHR0cC5yZXF1ZXN0cw==","count":3}]}`,
		`{"raw_message":"c3ludGhldGljLXNlY3JldA==","metadata":{"secret":{"source":"private"}}}`, `{"metrics":[{"name":"\u0068ttp.requests","count":1}],"errors":[{"code":"handler.panic","count":1,"frames":[{"id":"amos.health.serve_http.v1","trusted":true}]}]}`,
		`{"version":{"component":"amos.http-observability","public_baseline":"amos.http-observability.v1","verified":true},"frames":[{"symbol":"secret"}]}`,
	} {
		p, err := Construct([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		inspect(t, p)
		transport(t, p.Bytes())
	}
	invalid := []string{"", `null`, `[]`, `{} {}`, `{"metrics":null}`, `{"metrics":[null]}`, `{"errors":[true]}`, `{"metadata":{"x":1,"\u0078":2}}`, `{"metrics":[],"metrics":[]}`, `{"metadata":` + strings.Repeat("[", MaxDepth+2) + "0" + strings.Repeat("]", MaxDepth+2) + `}`, `{"x":"` + strings.Repeat("x", MaxStringBytes+1) + `"}`, `{"x":[` + strings.Repeat("0,", MaxArrayItems) + `0]}`}
	fieldLimit := map[string]any{}
	for i := 0; i <= MaxObjectFields; i++ {
		fieldLimit[strconv.Itoa(i)] = 0
	}
	encodedFields, err := json.Marshal(fieldLimit)
	if err != nil {
		t.Fatal(err)
	}
	invalid = append(invalid, string(encodedFields))
	manyNodes := make([]any, MaxArrayItems)
	for i := range manyNodes {
		manyNodes[i] = make([]int, 16)
	}
	encodedNodes, err := json.Marshal(map[string]any{"metadata": manyNodes})
	if err != nil {
		t.Fatal(err)
	}
	invalid = append(invalid, string(encodedNodes))
	for _, raw := range invalid {
		p, err := Construct([]byte(raw))
		if !errors.Is(err, ErrInvalid) || len(p.Bytes()) != 0 || p.Receipt() != (Receipt{}) {
			t.Fatal("invalid input yielded a preview")
		}
		if err.Error() != ErrInvalid.Error() {
			t.Fatal("error echoed raw input")
		}
	}
	for _, raw := range [][]byte{bytes.Repeat([]byte(" "), MaxInputBytes+1), {0xff}} {
		if _, err := Construct(raw); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid byte input accepted")
		}
	}
}
func FuzzSanitization(f *testing.F) {
	for _, seed := range []string{`{}`, `{"metrics":[{"name":"http.requests","count":1}]}`, `{"errors":[{"code":"handler.panic","count":1,"frames":[{"arguments":"private"}]}]}`, `{"metadata":{"nested":[{"source":"private"}]}}`, `{"metrics":[],"\u006det rics":[]}`, `{"raw_message":"c2VjcmV0"}`, `{"count":18446744073709551616}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := Construct(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || len(p.Bytes()) != 0 || p.Receipt() != (Receipt{}) {
				t.Fatal("invalid input exposed state")
			}
			return
		}
		inspect(t, p)
		transport(t, p.Bytes())
		q, err := Construct(raw)
		if err != nil || !bytes.Equal(p.Bytes(), q.Bytes()) || p.Receipt() != q.Receipt() {
			t.Fatal("projection is nondeterministic")
		}
	})
}
