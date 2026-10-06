package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRoutingOptionsAndFreezeThroughPublicApp(t *testing.T) {
	protocolHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Protocol", "mcp")
		w.WriteHeader(http.StatusAccepted)
	})
	businessHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Business", "matched")
		w.WriteHeader(http.StatusNoContent)
	})
	options := Options{
		ReadinessTimeout: time.Second,
		ShutdownTimeout:  time.Second,
		Protocol: &ProtocolHandlersV1{
			MCP: protocolHandler, AuthorizationServerMetadata: protocolHandler,
			ProtectedResourceMetadata: protocolHandler, Register: protocolHandler,
			Authorize: protocolHandler, Consent: protocolHandler, Token: protocolHandler, Revoke: protocolHandler,
		},
		Business: &BusinessRoutesV1{Home: businessHandler, Routes: []BusinessRouteV1{{Pattern: "/products/*", Handler: businessHandler}}},
	}
	application, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Protocol.MCP = nil
	options.Business.Routes[0].Pattern = "/mutated/*"
	handler := application.Handler()
	for _, test := range []struct {
		path, header string
		status       int
	}{
		{path: "/mcp", header: "X-Protocol", status: http.StatusAccepted},
		{path: "/products/one", header: "X-Business", status: http.StatusNoContent},
		{path: "/", header: "X-Business", status: http.StatusNoContent},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || response.Header().Get(test.header) == "" {
			t.Fatalf("GET %s = %d, headers=%v", test.path, response.Code, response.Header())
		}
	}
	if err := application.RegisterBusinessRoute(http.MethodGet, "/late", businessHandler); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("late registration error=%v, want ErrRoutesFrozen", err)
	}
}

func TestBusinessRouteConfigurationAndLegacyConflict(t *testing.T) {
	options := Options{
		ReadinessTimeout: time.Second,
		ShutdownTimeout:  time.Second,
		Business:         &BusinessRoutesV1{Home: http.NotFoundHandler()},
	}
	application, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.RegisterBusinessRoute(http.MethodGet, "/", http.NotFoundHandler()); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("legacy root registration error=%v, want conflict", err)
	}
}

func TestLateRouteErrorPrecedence(t *testing.T) {
	application, err := New(Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	application.Handler()
	if err := application.RegisterBusinessRoute(http.MethodGet, "/oauth/private", http.NotFoundHandler()); !errors.Is(err, ErrReservedRoute) {
		t.Fatalf("reserved late registration error=%v, want reserved", err)
	}
	if err := application.RegisterBusinessRoute(http.MethodGet, "/bad%2fpath", http.NotFoundHandler()); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("malformed late registration error=%v, want invalid", err)
	}
	if err := application.RegisterBusinessRoute(http.MethodGet, "/valid", http.NotFoundHandler()); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("valid late registration error=%v, want frozen", err)
	}
}
