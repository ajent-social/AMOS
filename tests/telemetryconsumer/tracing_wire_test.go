package telemetryconsumer_test

import (
	"bytes"
	"strings"
	"testing"

	tracewire "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func assertTracingGraph(t *testing.T, spans []*tracewire.Span) {
	t.Helper()
	if len(spans) != 4 {
		t.Fatalf("actual graph has %d spans want4", len(spans))
	}
	var server, backend, database, client *tracewire.Span
	for _, span := range spans {
		switch span.Name {
		case "database.transaction":
			database = span
		case "http.client":
			client = span
		case "http.server":
			if len(span.ParentSpanId) == 0 {
				server = span
			} else {
				backend = span
			}
		}
	}
	if server == nil || backend == nil || database == nil || client == nil {
		t.Fatal("actual graph missing required public/database/backend span")
	}
	if !bytes.Equal(database.ParentSpanId, server.SpanId) || !bytes.Equal(client.ParentSpanId, server.SpanId) || !bytes.Equal(backend.ParentSpanId, client.SpanId) {
		t.Fatal("actual graph parent edges differ from frozen sibling/mTLS graph")
	}
	for _, span := range spans {
		if !bytes.Equal(span.TraceId, server.TraceId) || bytes.Equal(span.TraceId, bytes.Repeat([]byte{0x11}, 16)) {
			t.Fatal("trace continuity changed or public forged trace admitted")
		}
		if span.Flags != 1 {
			t.Fatal("local sampled flag differs")
		}
	}
}
func assertTracingPrivacy(t *testing.T, sink *tracingCollector) {
	t.Helper()
	requests, invalid := sink.snapshot()
	if invalid {
		t.Fatal("invalid collector wire request")
	}
	for _, request := range requests {
		body, err := protojson.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		for _, canary := range []string{tracingCanary, tracingToken, "collector.test", "backend.test", "SELECT", "INSERT", "credential", "payload", "private=", "/work", "/rollback", "/private", "dHJhY2luZy1zZWNyZXQ="} {
			if strings.Contains(string(body), canary) {
				t.Fatal("private canary reached actual collector wire")
			}
		}
		assertTracingFields(t, request.ProtoReflect())
		for _, resource := range request.ResourceSpans {
			if resource.SchemaUrl != "" || resource.Resource == nil || len(resource.Resource.Attributes) != 1 || resource.Resource.Attributes[0].Key != "service.name" || resource.Resource.Attributes[0].Value.GetStringValue() != "amos" {
				t.Fatal("wire resource identity not fixed")
			}
			for _, scope := range resource.ScopeSpans {
				if scope.SchemaUrl != "" || scope.Scope == nil || scope.Scope.Name != "amos.tracing.v1" || scope.Scope.Version != "" || len(scope.Scope.Attributes) != 0 {
					t.Fatal("wire scope not fixed")
				}
				for _, span := range scope.Spans {
					if len(span.TraceId) != 16 || len(span.SpanId) != 8 || (len(span.ParentSpanId) != 0 && len(span.ParentSpanId) != 8) || span.StartTimeUnixNano == 0 || span.EndTimeUnixNano < span.StartTimeUnixNano || len(span.Events) != 0 || len(span.Links) != 0 || span.TraceState != "" || span.Flags > 1 {
						t.Fatal("wire span contains invalid identifiers/times/extra data")
					}
					allowed := map[string]bool{"amos.outcome": true}
					switch span.Name {
					case "http.server":
						allowed["amos.route"] = true
						if span.Kind != tracewire.Span_SPAN_KIND_SERVER {
							t.Fatal("server wire kind invalid")
						}
					case "http.client":
						allowed["amos.destination"] = true
						if span.Kind != tracewire.Span_SPAN_KIND_CLIENT {
							t.Fatal("client wire kind invalid")
						}
					case "database.transaction":
						if span.Kind != tracewire.Span_SPAN_KIND_INTERNAL {
							t.Fatal("database wire kind invalid")
						}
					default:
						t.Fatal("unknown wire span name")
					}
					if len(span.Attributes) != len(allowed) {
						t.Fatal("wire attribute count invalid")
					}
					seen := map[string]bool{}
					for _, item := range span.Attributes {
						if !allowed[item.Key] || seen[item.Key] || item.Value == nil {
							t.Fatal("unknown/duplicate wire attribute")
						}
						seen[item.Key] = true
						value := item.Value.GetStringValue()
						if item.Key == "amos.outcome" {
							if !map[string]bool{"ok": true, "error": true, "cancelled": true, "panic": true, "commit": true, "rollback": true, "unavailable": true}[value] {
								t.Fatal("unknown wire outcome")
							}
						} else if value != "other" && value != "unmatched" && (len(value) != 3 || (value[0] != 'r' && value[0] != 'o') || value[1] < '0' || value[1] > '3' || value[2] < '0' || value[2] > '9') {
							t.Fatal("nonordinal wire label")
						}
					}
					if span.Status != nil && (span.Status.Message != "" || (span.Status.Code != tracewire.Status_STATUS_CODE_UNSET && span.Status.Code != tracewire.Status_STATUS_CODE_ERROR)) {
						t.Fatal("wire status contains diagnostic material")
					}
				}
			}
		}
	}
}

// Inspect every populated field recursively, including protobuf unknown fields.
// This proves absence beyond the particular canaries inspected above.
func assertTracingFields(t *testing.T, message protoreflect.Message) {
	t.Helper()
	if len(message.GetUnknown()) != 0 {
		t.Fatal("unknown protobuf fields reached wire")
	}
	allowed := map[protoreflect.Name]map[protoreflect.Name]bool{
		"ExportTraceServiceRequest": {"resource_spans": true},
		"ResourceSpans":             {"resource": true, "scope_spans": true},
		"Resource":                  {"attributes": true},
		"ScopeSpans":                {"scope": true, "spans": true},
		"InstrumentationScope":      {"name": true},
		"Span":                      {"trace_id": true, "span_id": true, "parent_span_id": true, "flags": true, "name": true, "kind": true, "start_time_unix_nano": true, "end_time_unix_nano": true, "attributes": true, "status": true},
		"KeyValue":                  {"key": true, "value": true},
		"AnyValue":                  {"string_value": true},
		"Status":                    {"code": true},
	}
	fields, ok := allowed[message.Descriptor().Name()]
	if !ok {
		t.Fatal("unapproved wire message kind")
	}
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if !fields[field.Name()] {
			t.Fatal("unapproved populated protobuf field")
		}
		if field.IsMap() {
			t.Fatal("arbitrary wire map")
		}
		if field.Kind() == protoreflect.MessageKind {
			if field.IsList() {
				list := value.List()
				for i := 0; i < list.Len(); i++ {
					assertTracingFields(t, list.Get(i).Message())
				}
			} else {
				assertTracingFields(t, value.Message())
			}
		}
		return true
	})
}
