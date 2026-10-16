package apphost

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/app"
	delivery "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/ui/authpassword"
)

func developmentCertificate(t *testing.T, change func(*x509.Certificate)) ([]byte, []byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Development test identity"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true}
	if change != nil {
		change(template)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), leaf
}
func TestDevelopmentTLSRejectsIdentityBeforeDependencies(t *testing.T) {
	cert, key, _ := developmentCertificate(t, nil)
	cfg := localConfig(t, "invalid-private-database-url", "https://127.0.0.1:8443")
	if host, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: cfg, CertificatePEM: cert, PrivateKeyPEM: key}); host != nil || err != ErrLocalDependency {
		t.Fatal("control did not reach dependency boundary", err)
	}
	for name, change := range map[string]func(*x509.Certificate){"expired": func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Second) }, "not yet valid": func(c *x509.Certificate) {
		c.NotBefore = time.Now().Add(time.Hour)
		c.NotAfter = time.Now().Add(2 * time.Hour)
	}, "wrong SAN": func(c *x509.Certificate) { c.IPAddresses = []net.IP{net.ParseIP("192.0.2.1")} }, "DNS only": func(c *x509.Certificate) { c.IPAddresses = nil; c.DNSNames = []string{"127.0.0.1"} }, "no explicit server usage": func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageAny} }, "no signing usage": func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageKeyEncipherment }} {
		t.Run(name, func(t *testing.T) {
			c, k, _ := developmentCertificate(t, change)
			if host, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: cfg, CertificatePEM: c, PrivateKeyPEM: k}); host != nil || err != ErrLocalConfiguration {
				t.Fatal("invalid identity reached dependencies", err)
			}
		})
	}
	_, otherKey, _ := developmentCertificate(t, nil)
	for name, pair := range map[string][2][]byte{"missing certificate": {nil, key}, "missing key": {cert, nil}, "mismatched key": {cert, otherKey}, "malformed": {[]byte("not PEM"), key}, "extra identity": {append(bytes.Clone(cert), cert...), key}, "extra key": {cert, append(bytes.Clone(key), key...)}, "prefix diagnostic": {append([]byte("untrusted prefix\n"), cert...), key}, "trailing data": {append(bytes.Clone(cert), []byte("junk")...), key}, "oversized": {make([]byte, 64<<10+1), key}} {
		t.Run(name, func(t *testing.T) {
			if host, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: cfg, CertificatePEM: pair[0], PrivateKeyPEM: pair[1]}); host != nil || err != ErrLocalConfiguration {
				t.Fatal("bad material reached dependencies", err)
			}
		})
	}
	for _, origin := range []string{"http://127.0.0.1:8443", "https://127.0.0.1:8443/", "https://example.invalid:8443", "https://0.0.0.0:8443", "https://10.0.0.1:8443"} {
		bad := cfg
		bad.Origin = origin
		if host, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: bad, CertificatePEM: cert, PrivateKeyPEM: key}); host != nil || err != ErrLocalConfiguration {
			t.Fatal("invalid origin/SAN reached dependencies", err)
		}
	}
	bad := cfg
	bad.Billing = &LocalBillingConfig{}
	if _, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: bad, CertificatePEM: cert, PrivateKeyPEM: key}); err != ErrLocalConfiguration {
		t.Fatal("billing accepted")
	}
	bad = cfg
	bad.AuthTheme = &authpassword.Theme{BrandName: "Example", StylesheetURL: "https://example.invalid/theme.css"}
	if _, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: bad, CertificatePEM: cert, PrivateKeyPEM: key}); err != ErrLocalConfiguration {
		t.Fatal("theme validation bypassed")
	}
}
func TestDevelopmentTLSRejectsNonExactPlainTCPBinding(t *testing.T) {
	c, k, _ := developmentCertificate(t, nil)
	origin, _ := delivery.ParseDevelopmentTLSOrigin("https://127.0.0.1:8443")
	identity, err := parseDevelopmentTLSIdentity(c, k, origin)
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.New(app.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, bind := range []string{"0.0.0.0:0", "127.0.0.2:0", "127.0.0.1:0"} {
		t.Run(bind, func(t *testing.T) {
			listener, err := net.Listen("tcp4", bind)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			configured := *origin
			if bind != "127.0.0.1:0" {
				_, port, _ := net.SplitHostPort(listener.Addr().String())
				configured.Host = "127.0.0.1:" + port
			}
			h := &Host{app: a, origin: &configured, tlsIdentity: identity}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := h.Serve(ctx, listener); err != ErrLocalConfiguration {
				t.Fatal("non-exact listener accepted", err)
			}
		})
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	configured := *origin
	configured.Host = listener.Addr().String()
	h := &Host{app: a, origin: &configured, tlsIdentity: identity}
	wrapped := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS13})
	if err := h.Serve(context.Background(), wrapped); err != ErrLocalConfiguration {
		t.Fatal("caller TLS wrapper accepted")
	}
}
func developmentClient(t *testing.T, leaf *x509.Certificate) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13, ClientSessionCache: tls.NewLRUClientSessionCache(4)}, MaxIdleConnsPerHost: 1}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func startDevelopmentHost(t *testing.T, change func(*x509.Certificate)) (*Host, LocalConfig, *http.Client, net.Listener, *x509.Certificate) {
	t.Helper()
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "https://" + listener.Addr().String()
	cfg := localConfig(t, dsn, origin)
	certificate, key, leaf := developmentCertificate(t, change)
	h, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: cfg, CertificatePEM: certificate, PrivateKeyPEM: key})
	if err != nil {
		t.Fatal("TLS host construction failed", err)
	}
	// Serving must use a parsed private identity, never the caller's mutable buffers.
	clear(certificate)
	clear(key)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("TLS host failed bounded shutdown")
		}
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	return h, cfg, developmentClient(t, leaf), listener, leaf
}
func TestDevelopmentTLSActualAccountTodoLifecycleAndBoundaries(t *testing.T) {
	host, cfg, client, listener, leaf := startDevelopmentHost(t, nil)
	origin := cfg.Origin
	status, page := response(t, client, "GET", origin+"/signin", "", nil)
	if status != 200 || !strings.Contains(page, "Sign in") {
		t.Fatal("TLS page unavailable")
	}
	if err := host.Serve(context.Background(), listener); !errors.Is(err, app.ErrAlreadyServing) {
		t.Fatal("concurrent serve stole listener", err)
	}
	status, _ = response(t, client, "GET", origin+"/healthz", "", nil)
	if status != 200 {
		t.Fatal("first serve disrupted")
	}
	for _, path := range []string{"/healthz", "/readyz", "/signin", "/todos"} {
		request, err := http.NewRequest("GET", origin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = "foreign.invalid:8443"
		request.Header.Set("X-Forwarded-Host", strings.TrimPrefix(origin, "https://"))
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("Forwarded", "host="+strings.TrimPrefix(origin, "https://")+";proto=https")
		res, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("forwarded headers rescued foreign Host")
		}
	}
	emailAddress := "tls-owner@example.test"
	phrase := "fresh development TLS phrase 2026!"
	status, _ = response(t, client, "POST", origin+"/signup", origin, url.Values{"email": {emailAddress}, "password": {phrase}})
	if status != 202 {
		t.Fatal("TLS signup failed", status)
	}
	var action *url.URL
	deadline := time.Now().Add(5 * time.Second)
	for action == nil && time.Now().Before(deadline) {
		files, err := os.ReadDir(cfg.MailDirectory)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) > 0 {
			body, err := os.ReadFile(filepath.Join(cfg.MailDirectory, files[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			var captured struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(body, &captured) != nil {
				t.Fatal("private capture malformed")
			}
			first, _, _ := strings.Cut(captured.Text, "\n")
			action, err = url.Parse(first)
			if err != nil {
				t.Fatal("private action malformed")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if action == nil || action.Scheme != "https" || action.Host != strings.TrimPrefix(origin, "https://") {
		t.Fatal("HTTPS captured verification unavailable")
	}
	status, _ = response(t, client, "GET", action.String(), "", nil)
	if status != 200 {
		t.Fatal("verification preview failed")
	}
	status, _ = response(t, client, "POST", origin+"/verify-email", origin, url.Values{"challenge": {action.Query().Get("challenge")}, "token": {action.Query().Get("token")}})
	if status != 200 {
		t.Fatal("verification failed", status)
	}
	request, err := http.NewRequest("POST", origin+"/auth", strings.NewReader(url.Values{"email": {emailAddress}, "password": {phrase}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", origin)
	res, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 303 || res.Header.Get("X-Request-ID") == "" || res.TLS == nil || res.TLS.Version != tls.VersionTLS13 || !bytes.Equal(res.TLS.PeerCertificates[0].Raw, leaf.Raw) {
		t.Fatal("native TLS sign-in unavailable")
	}
	found := false
	for _, cookie := range res.Cookies() {
		if cookie.Name == "amos_dev_session" {
			t.Fatal("insecure development cookie issued")
		}
		if cookie.Name == "__Host-amos_session" {
			found = true
			if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("session cookie boundary weakened")
			}
		}
	}
	if !found {
		t.Fatal("secure host-only cookie missing")
	}
	status, body := response(t, client, "GET", origin+"/todos", "", nil)
	if status != 200 {
		t.Fatal("personal todo page unavailable")
	}
	match := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("session CSRF unavailable")
	}
	csrf := match[1]
	for _, vector := range []string{"missing csrf", "wrong origin", "duplicate origin", "foreign referer", "forwarded spoof"} {
		form := url.Values{"title": {"Rejected development todo"}, "_csrf": {csrf}}
		if vector == "missing csrf" {
			form.Del("_csrf")
		}
		r, err := http.NewRequest("POST", origin+"/todos", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		switch vector {
		case "wrong origin", "forwarded spoof":
			r.Header.Set("Origin", "https://foreign.invalid:8443")
		case "duplicate origin":
			r.Header.Add("Origin", origin)
		case "foreign referer":
			r.Header.Del("Origin")
			r.Header.Set("Referer", "https://foreign.invalid:8443/todos")
		}
		r.Header.Set("X-Forwarded-Host", strings.TrimPrefix(origin, "https://"))
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("CSRF/origin vector %s accepted with %d", vector, res.StatusCode)
		}
	}
	status, _ = response(t, client, "POST", origin+"/todos", origin, url.Values{"title": {"Development TLS todo"}, "_csrf": {csrf}})
	if status != 303 {
		t.Fatal("TLS todo mutation failed", status)
	}
	status, body = response(t, client, "GET", origin+"/todos", "", nil)
	if status != 200 || !strings.Contains(body, "Development TLS todo") || strings.Contains(body, "Rejected development todo") {
		t.Fatal("todo authority failure")
	}
	status, _ = response(t, client, "POST", origin+"/signout", origin, url.Values{"_csrf": {csrf}})
	if status != 204 {
		t.Fatal("TLS signout failed", status)
	}
	status, _ = response(t, client, "GET", origin+"/todos", "", nil)
	if status != 401 {
		t.Fatal("session survived signout")
	}
	// Normal certificate verification is mandatory, with no trust or IP override.
	for _, config := range []*tls.Config{{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS13}, {RootCAs: client.Transport.(*http.Transport).TLSClientConfig.RootCAs, ServerName: "192.0.2.1", MinVersion: tls.VersionTLS13}, {RootCAs: client.Transport.(*http.Transport).TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}} {
		transport := &http.Transport{TLSClientConfig: config}
		badClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
		if response, err := badClient.Get(origin + "/healthz"); err == nil {
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			t.Fatal("invalid trust/IP/TLS version accepted")
		}
		transport.CloseIdleConnections()
	}
	plain := &http.Client{Timeout: 3 * time.Second}
	if res, err := plain.Get(strings.Replace(origin, "https://", "http://", 1) + "/signin"); err == nil {
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode == 200 || strings.Contains(string(body), "<form") {
			t.Fatal("plaintext obtained application content")
		}
	}
}
func TestDevelopmentTLSExpiryOnResumedPersistentConnection(t *testing.T) {
	_, cfg, client, _, leaf := startDevelopmentHost(t, func(c *x509.Certificate) { c.NotAfter = time.Now().Add(10 * time.Second) })
	// Hold only the test client clock before expiry so it will offer the cached
	// ticket after real server expiry. Certificate/root/IP verification stays on.
	verifiedClientTime := time.Now()
	client.Transport.(*http.Transport).TLSClientConfig.Time = func() time.Time { return verifiedClientTime }
	for i := 0; i < 2; i++ {
		res, err := client.Get(cfg.Origin + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal("valid identity rejected")
		}
		if i == 0 {
			client.Transport.(*http.Transport).CloseIdleConnections()
		} else if !res.TLS.DidResume {
			t.Fatal("resumed TLS connection not exercised")
		}
	}
	time.Sleep(max(time.Until(leaf.NotAfter.Add(30*time.Millisecond)), 0))
	res, err := client.Get(cfg.Origin + "/healthz")
	if err != nil {
		t.Fatal("persistent connection unexpectedly unavailable", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 503 || !res.TLS.DidResume || strings.Contains(string(body), `"status":"ok"`) {
		t.Fatal("expired identity served on resumed persistent connection")
	}
	_ = res.Body.Close()
	client.Transport.(*http.Transport).CloseIdleConnections()
	if response, err := client.Get(cfg.Origin + "/healthz"); err == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		t.Fatalf("expired cached-session handshake accepted: resumed=%v", response.TLS.DidResume)
	}
}
func TestDevelopmentTLSGuardRejectsPlaintextBeforeAnyRoute(t *testing.T) {
	cert, key, _ := developmentCertificate(t, nil)
	origin, _ := delivery.ParseDevelopmentTLSOrigin("https://127.0.0.1:8443")
	identity, err := parseDevelopmentTLSIdentity(cert, key, origin)
	if err != nil {
		t.Fatal(err)
	}
	h := &Host{origin: origin, tlsIdentity: identity}
	calls := 0
	guard := h.developmentTLSGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	for _, tlsState := range []*tls.ConnectionState{nil, {Version: tls.VersionTLS12}} {
		r := httptest.NewRequest("GET", origin.String()+"/healthz", nil)
		r.TLS = tlsState
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != 400 || calls != 0 {
			t.Fatal("transport guard bypassed")
		}
	}
}

func TestDevelopmentTLSShutdownForceClosesAfterBound(t *testing.T) {
	dsn := localDatabase(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	cfg := localConfig(t, dsn, "https://"+listener.Addr().String())
	cert, key, leaf := developmentCertificate(t, nil)
	host, err := NewDevelopmentTLS(context.Background(), DevelopmentTLSConfig{Local: cfg, CertificatePEM: cert, PrivateKeyPEM: key})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close() }()
	started := make(chan struct{})
	requestStopped := make(chan struct{})
	if err := host.app.RegisterBusinessRoute("GET", "/slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestStopped)
	})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx, listener) }()
	client := developmentClient(t, leaf)
	client.Timeout = 10 * time.Second
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		if res, err := client.Get(cfg.Origin + "/slow"); err == nil {
			_ = res.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("slow TLS request did not start")
	}
	begin := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("force-close deadline not exercised", err)
		}
		if time.Since(begin) > 6*time.Second {
			t.Fatal("shutdown exceeded bound")
		}
	case <-time.After(6 * time.Second):
		_ = listener.Close()
		t.Fatal("shutdown failed to force close")
	}
	select {
	case <-requestStopped:
	case <-time.After(time.Second):
		t.Fatal("force-close did not cancel request")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("client retained force-closed connection")
	}
}
