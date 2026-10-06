package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func routingOptions() Options {
	return Options{ReadinessTimeout: time.Second, ShutdownTimeout: time.Second}
}

type routingTypedNilHandler struct{}

func (*routingTypedNilHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func protocolBundle(handler http.Handler) *ProtocolHandlersV1 {
	return &ProtocolHandlersV1{
		MCP: handler, AuthorizationServerMetadata: handler,
		ProtectedResourceMetadata: handler, Register: handler,
		Authorize: handler, Consent: handler, Token: handler, Revoke: handler,
	}
}

func request(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.RequestURI = target
	return req
}

func TestProtocolBundleValidation(t *testing.T) {
	funcNil := func() http.Handler { var handler *routingTypedNilHandler; return handler }
	for _, test := range []struct {
		name    string
		bundle  *ProtocolHandlersV1
		wantErr error
	}{
		{name: "nil disables protocol"},
		{name: "empty bundle", bundle: &ProtocolHandlersV1{}, wantErr: ErrInvalidOptions},
		{name: "partial bundle", bundle: &ProtocolHandlersV1{MCP: http.NotFoundHandler()}, wantErr: ErrInvalidOptions},
		{name: "typed nil", bundle: protocolBundle(funcNil()), wantErr: ErrInvalidOptions},
		{name: "complete bundle", bundle: protocolBundle(http.NotFoundHandler())},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := routingOptions()
			options.Protocol = test.bundle
			_, err := New(options)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestProtocolSlotsPreserveEveryValidMethodAndRequest(t *testing.T) {
	type observed struct {
		method, uri, body, header string
	}
	var got observed
	slot := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			body, _ := io.ReadAll(req.Body)
			got = observed{method: req.Method, uri: req.URL.RequestURI(), body: string(body), header: req.Header.Get("X-Original")}
			w.Header().Set("X-Protocol", name)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, name)
		})
	}
	options := routingOptions()
	options.Protocol = &ProtocolHandlersV1{
		MCP: slot("mcp"), AuthorizationServerMetadata: slot("authorization-metadata"),
		ProtectedResourceMetadata: slot("protected-resource-metadata"), Register: slot("register"),
		Authorize: slot("authorize"), Consent: slot("consent"), Token: slot("token"), Revoke: slot("revoke"),
	}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, slot string }{
		{path: "/mcp", slot: "mcp"},
		{path: "/.well-known/oauth-authorization-server", slot: "authorization-metadata"},
		{path: "/.well-known/oauth-protected-resource/mcp", slot: "protected-resource-metadata"},
		{path: "/oauth/register", slot: "register"},
		{path: "/oauth/authorize", slot: "authorize"},
		{path: "/oauth/consent", slot: "consent"},
		{path: "/oauth/token", slot: "token"},
		{path: "/oauth/revoke", slot: "revoke"},
	} {
		for _, method := range []string{"GET", "HEAD", "POST", "PATCH", "DELETE", "OPTIONS", "mIxEd-Extension"} {
			req := request(method, test.path+"?cursor=2", strings.NewReader("original-body"))
			req.Header.Set("X-Original", "kept")
			response := httptest.NewRecorder()
			runtime.ServeHTTP(response, req)
			if response.Code != http.StatusAccepted || response.Header().Get("X-Protocol") != test.slot || response.Body.String() != test.slot {
				t.Fatalf("%s %s response = %d, %v, %q", method, test.path, response.Code, response.Header(), response.Body.String())
			}
			if got != (observed{method: method, uri: test.path + "?cursor=2", body: "original-body", header: "kept"}) {
				t.Fatalf("%s %s observed = %+v", method, test.path, got)
			}
		}
	}
}

func TestProtocolAliasesReturnBadRequestWhenBoundOrAbsent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, bound := range []bool{false, true} {
		options := routingOptions()
		if bound {
			options.Protocol = protocolBundle(handler)
		}
		runtime, err := New(options)
		if err != nil {
			t.Fatal(err)
		}
		for _, alias := range []string{"/mcp/", "/%6dcp", "/oauth/%74oken", "/.well-known/oauth-protected-resource/mcp/"} {
			response := httptest.NewRecorder()
			runtime.ServeHTTP(response, request(http.MethodGet, alias, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("bound=%t alias=%q status=%d, want 400", bound, alias, response.Code)
			}
		}
	}
}

func TestBusinessManifestValidationAndSnapshot(t *testing.T) {
	var nilHandler *routingTypedNilHandler
	response := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "original") })
	for _, test := range []struct {
		name     string
		business *BusinessRoutesV1
		wantErr  error
	}{
		{name: "nil disables manifest"},
		{name: "empty manifest", business: &BusinessRoutesV1{}, wantErr: ErrInvalidOptions},
		{name: "typed nil home", business: &BusinessRoutesV1{Home: nilHandler}, wantErr: ErrInvalidOptions},
		{name: "typed nil route", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/items", Handler: nilHandler}}}, wantErr: ErrInvalidOptions},
		{name: "reserved", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/billing/plan", Handler: response}}}, wantErr: ErrReservedRoute},
		{name: "trailing slash", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/items/", Handler: response}}}, wantErr: ErrInvalidRoute},
		{name: "encoded pattern", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/it%65ms", Handler: response}}}, wantErr: ErrInvalidRoute},
		{name: "interior wildcard", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/items/*/detail", Handler: response}}}, wantErr: ErrInvalidRoute},
		{name: "duplicate", business: &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/items", Handler: response}, {Pattern: "/items", Handler: response}}}, wantErr: ErrRouteConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := routingOptions()
			options.Business = test.business
			_, err := New(options)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, test.wantErr)
			}
		})
	}

	configured := &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/items", Handler: response}}}
	options := routingOptions()
	options.Business = configured
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	configured.Routes[0] = BusinessRouteV1{Pattern: "/changed", Handler: http.NotFoundHandler()}
	for _, target := range []string{"/items", "/changed"} {
		response := httptest.NewRecorder()
		runtime.Handler().ServeHTTP(response, request(http.MethodGet, target, nil))
		want := http.StatusNotFound
		if target == "/items" {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("GET %s = %d, want %d", target, response.Code, want)
		}
	}
}

func TestMethodNeutralBusinessMatchingAndLegacyCompatibility(t *testing.T) {
	response := func(body string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, body+"|"+req.Method+"|"+req.URL.RawQuery+"|"+req.Header.Get("X-Input"))
		})
	}
	options := routingOptions()
	options.Business = &BusinessRoutesV1{
		Home: response("home"),
		Routes: []BusinessRouteV1{
			{Pattern: "/records/*", Handler: response("short")},
			{Pattern: "/records/admin/*", Handler: response("long")},
			{Pattern: "/records/admin/health", Handler: response("exact")},
		},
	}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RegisterBusinessRoute("GET", "/legacy/*", response("legacy")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path, want string
	}{
		{http.MethodGet, "/", "home|GET|q=1|kept"},
		{http.MethodHead, "/records/41", "short|HEAD|q=1|kept"},
		{http.MethodPost, "/records/42", "short|POST|q=1|kept"},
		{http.MethodPatch, "/records", "short|PATCH|q=1|kept"},
		{http.MethodOptions, "/records/42", "short|OPTIONS|q=1|kept"},
		{"customVerb", "/records/admin/42", "long|customVerb|q=1|kept"},
		{http.MethodDelete, "/records/admin/health", "exact|DELETE|q=1|kept"},
		{http.MethodGet, "/records-archive", ""},
		{http.MethodGet, "/legacy", ""}, // legacy prefixes remain descendant-only
		{http.MethodGet, "/legacy/item", "legacy|GET|q=1|kept"},
	} {
		req := request(test.method, test.path+"?q=1", nil)
		req.Header.Set("X-Input", "kept")
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, req)
		if test.want == "" {
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s status=%d, want 404", test.method, test.path, response.Code)
			}
			continue
		}
		if response.Code != http.StatusOK || response.Body.String() != test.want {
			t.Fatalf("%s %s = %d %q, want 200 %q", test.method, test.path, response.Code, response.Body.String(), test.want)
		}
	}
}

func TestLegacyAndManifestConflictsIgnoreMethod(t *testing.T) {
	handler := http.NotFoundHandler()
	options := routingOptions()
	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/records/*", Handler: handler}}}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, pattern string
		wantErr         error
	}{
		{http.MethodPost, "/records/42", ErrRouteConflict},
		{http.MethodPost, "/records", ErrRouteConflict},
		{http.MethodPost, "/record", nil},
	} {
		err := runtime.RegisterBusinessRoute(test.method, test.pattern, handler)
		if !errors.Is(err, test.wantErr) {
			t.Fatalf("RegisterBusinessRoute(%q,%q) error=%v, want %v", test.method, test.pattern, err, test.wantErr)
		}
	}

	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/exact/item", Handler: handler}}}
	runtime, err = New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RegisterBusinessRoute(http.MethodPost, "/exact/*", handler); !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("new exact/legacy descendant overlap error=%v, want conflict", err)
	}
}

func TestNewBusinessAliasesReturnBadRequestAndLegacyAliasesRemainCompatible(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	options := routingOptions()
	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/records/*", Handler: handler}}}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"/records/", "/%72ecords/42", "/records/%34%32"} {
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, request(http.MethodGet, alias, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("new business alias %q status=%d, want 400", alias, response.Code)
		}
	}
	legacyOptions := routingOptions()
	legacy, err := New(legacyOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.RegisterBusinessRoute(http.MethodGet, "/records", handler); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"/records/", "/%72ecords"} {
		response := httptest.NewRecorder()
		legacy.ServeHTTP(response, request(http.MethodGet, alias, nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("legacy alias %q status=%d, want 204", alias, response.Code)
		}
	}
}

func TestRegistrationFreezeErrorOrderingAndServeBoundary(t *testing.T) {
	handler := http.NotFoundHandler()
	options := routingOptions()
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Handler()
	if err := runtime.RegisterBusinessRoute(http.MethodGet, "/billing/private", handler); !errors.Is(err, ErrReservedRoute) {
		t.Fatalf("reserved registration after freeze error=%v, want reserved", err)
	}
	if err := runtime.RegisterBusinessRoute(http.MethodGet, "/bad%2fpath", handler); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("malformed registration after freeze error=%v, want invalid", err)
	}
	if err := runtime.RegisterBusinessRoute(http.MethodGet, "/valid", handler); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("valid registration after Handler error=%v, want frozen", err)
	}

	for _, test := range []struct {
		name     string
		ctx      context.Context
		listener net.Listener
	}{
		{name: "nil context", listener: closedListener(t)},
		{name: "nil listener", ctx: context.Background()},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh, err := New(routingOptions())
			if err != nil {
				t.Fatal(err)
			}
			if err := fresh.Serve(test.ctx, test.listener); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("Serve() error=%v, want invalid options", err)
			}
			if err := fresh.RegisterBusinessRoute(http.MethodGet, "/still-open", handler); err != nil {
				t.Fatalf("invalid Serve froze routes: %v", err)
			}
		})
	}

	valid, err := New(routingOptions())
	if err != nil {
		t.Fatal(err)
	}
	listener := closedListener(t)
	if err := valid.Serve(context.Background(), listener); err == nil {
		t.Fatal("Serve on closed listener succeeded")
	}
	if err := valid.RegisterBusinessRoute(http.MethodGet, "/after-listener-error", handler); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("registration after listener failure error=%v, want frozen", err)
	}

	shutdown, err := New(routingOptions())
	if err != nil {
		t.Fatal(err)
	}
	shutdownListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := shutdown.Serve(ctx, shutdownListener); err != nil {
		t.Fatalf("Serve with canceled context: %v", err)
	}
	if err := shutdown.RegisterBusinessRoute(http.MethodGet, "/after-shutdown", handler); !errors.Is(err, ErrRoutesFrozen) {
		t.Fatalf("registration after shutdown error=%v, want frozen", err)
	}
}

func closedListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return listener
}

func TestConcurrentRegistrationAndHandlerExposureFreezeAtomically(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		runtime, err := New(routingOptions())
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var registrationErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			registrationErr = runtime.RegisterBusinessRoute(http.MethodGet, "/race", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		}()
		go func() {
			defer wg.Done()
			<-start
			runtime.Handler()
		}()
		close(start)
		wg.Wait()
		if !errors.Is(registrationErr, ErrRoutesFrozen) && registrationErr != nil {
			t.Fatalf("concurrent registration error=%v", registrationErr)
		}
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, request(http.MethodGet, "/race", nil))
		if registrationErr == nil && response.Code != http.StatusNoContent || errors.Is(registrationErr, ErrRoutesFrozen) && response.Code != http.StatusNotFound {
			t.Fatalf("registration error=%v, response=%d", registrationErr, response.Code)
		}
	}
}

func TestBusinessPrefixCollisionAndReservedAliasBoundaries(t *testing.T) {
	handler := http.NotFoundHandler()
	options := routingOptions()
	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{
		{Pattern: "/records/*", Handler: handler},
		{Pattern: "/records/admin/*", Handler: handler},
		{Pattern: "/records/admin/health", Handler: handler},
	}}
	if _, err := New(options); err != nil {
		t.Fatalf("intentional nested route overrides rejected: %v", err)
	}
	for _, path := range []string{"/oauth/unknown", "/oauth/unknown/child", "/BILLING/plan", "/%62illing/plan"} {
		runtime, err := New(routingOptions())
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, request(http.MethodGet, path, nil))
		want := http.StatusServiceUnavailable
		if response.Code != want {
			t.Fatalf("reserved path %q status=%d, want %d", path, response.Code, want)
		}
	}
}

func TestLegacyRootRemainsAvailableWithoutConfiguredHome(t *testing.T) {
	options := routingOptions()
	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/catalog", Handler: http.NotFoundHandler()}}}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RegisterBusinessRoute(http.MethodGet, "/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})); err != nil {
		t.Fatalf("legacy root registration without configured home: %v", err)
	}
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, request(http.MethodGet, "/", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("legacy root status=%d, want 202", response.Code)
	}
}

func TestRealHTTPProtocolRoutePreservesMixedCaseMethod(t *testing.T) {
	type observation struct{ slot, method string }
	got := make(chan observation, 8)
	slot := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			got <- observation{slot: name, method: req.Method}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	options := routingOptions()
	options.Protocol = &ProtocolHandlersV1{
		MCP: slot("mcp"), AuthorizationServerMetadata: slot("authorization-metadata"),
		ProtectedResourceMetadata: slot("protected-resource-metadata"), Register: slot("register"),
		Authorize: slot("authorize"), Consent: slot("consent"), Token: slot("token"), Revoke: slot("revoke"),
	}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)
	for _, test := range []struct{ path, slot string }{
		{"/mcp", "mcp"},
		{"/.well-known/oauth-authorization-server", "authorization-metadata"},
		{"/.well-known/oauth-protected-resource/mcp", "protected-resource-metadata"},
		{"/oauth/register", "register"}, {"/oauth/authorize", "authorize"},
		{"/oauth/consent", "consent"}, {"/oauth/token", "token"}, {"/oauth/revoke", "revoke"},
	} {
		req, err := http.NewRequest("mIxEd-Extension", server.URL+test.path+"?x=1", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("%s HTTP status=%d, want 204", test.path, response.StatusCode)
		}
		if actual := <-got; actual != (observation{slot: test.slot, method: "mIxEd-Extension"}) {
			t.Fatalf("%s handler observed %+v", test.path, actual)
		}
	}
}

func TestRealHTTPBusinessRouteAndStrictAliasBoundary(t *testing.T) {
	type observation struct{ method, query, body, header string }
	got := make(chan observation, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		got <- observation{method: req.Method, query: req.URL.RawQuery, body: string(body), header: req.Header.Get("X-Original")}
		w.Header().Set("X-Business", "preserved")
		w.WriteHeader(http.StatusAccepted)
	})
	options := routingOptions()
	options.Business = &BusinessRoutesV1{Routes: []BusinessRouteV1{{Pattern: "/records/*", Handler: handler}}}
	runtime, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest("mIxEd-Extension", server.URL+"/records/42?cursor=3", strings.NewReader("request-body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Original", "kept")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusAccepted || response.Header.Get("X-Business") != "preserved" {
		t.Fatalf("business response status=%d headers=%v", response.StatusCode, response.Header)
	}
	if observed := <-got; observed != (observation{method: "mIxEd-Extension", query: "cursor=3", body: "request-body", header: "kept"}) {
		t.Fatalf("business handler observed %+v", observed)
	}
	alias, err := http.NewRequest(http.MethodGet, server.URL+"/records/%34%32", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasResponse, err := server.Client().Do(alias)
	if err != nil {
		t.Fatal(err)
	}
	_ = aliasResponse.Body.Close()
	if aliasResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("encoded business alias status=%d, want 400", aliasResponse.StatusCode)
	}
}
