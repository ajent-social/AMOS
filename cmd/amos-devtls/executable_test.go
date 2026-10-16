package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ajent-social/amos/billing/reconcile"
	reference "github.com/ajent-social/amos/examples/reference/migrations"
	"github.com/ajent-social/amos/identity/federation"
	"github.com/ajent-social/amos/identity/mfa"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

func testDatabase(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("AMOS_TEST_DATABASE_URL")
	if raw == "" {
		t.Fatal("required PostgreSQL fixture unavailable")
	}
	base, err := url.Parse(raw)
	if err != nil {
		t.Fatal("invalid PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error("fixture close failed")
		}
	})
	name := "amos_devtls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := pool.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal("isolated database creation failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error("owned database cleanup failed")
		}
	})
	base.Path = "/" + name
	base.RawQuery = "sslmode=disable"
	base.Host = net.JoinHostPort("127.0.0.1", base.Port())
	user := base.User.Username()
	base.User = url.UserPassword(user, "synthetic-fixture-password")
	dsn := base.String()
	if !validDatabaseURL(dsn) {
		t.Fatal("explicit fixture URL invalid")
	}
	db, err := storage.Open(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error("database close failed")
		}
	}()
	ingress, err := migrations.BillingWebhookIngress(9)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := migrations.RuntimeBinding(10)
	if err != nil {
		t.Fatal(err)
	}
	magic, err := migrations.MagicBrowserBinding(11)
	if err != nil {
		t.Fatal(err)
	}
	assurance, err := migrations.SessionAssurance(12)
	if err != nil {
		t.Fatal(err)
	}
	factors, err := mfa.Fragment(13)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := migrations.MFAProtection(14)
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, err := reconcile.Schema(15)
	if err != nil {
		t.Fatal(err)
	}
	federationFlow, err := federation.Fragment(16)
	if err != nil {
		t.Fatal(err)
	}
	invocations, err := migrations.OperationInvocations(17)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := migrations.Core(reference.Fragment(), ingress, binding, magic, assurance, factors, limits, reconciliation, federationFlow, invocations)
	if err != nil {
		t.Fatal(err)
	}
	if storage.Migrate(ctx, db, registry) != nil {
		t.Fatal("owned database migration failed")
	}
	return dsn
}
func testIdentity(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic development fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
}
func TestExecutableTLSLifecycle(t *testing.T) {
	if os.Getenv("AMOS_TEST_DATABASE_URL") == "" {
		t.Fatal("required PostgreSQL fixture unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "amos-devtls")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("executable build failed: %s", output)
	}
	dir := privateFiles(t)
	cfg := validConfig()
	config := testDatabase(t)
	cfg.DatabaseURL = config
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Origin = "https://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cert, key := testIdentity(t)
	for name, body := range map[string][]byte{"runtime.json": configBytes(t, cfg), "certificate.pem": cert, "private-key.pem": key} {
		if os.WriteFile(filepath.Join(dir, name), body, 0600) != nil {
			t.Fatal("fixture write failed")
		}
	}
	command := exec.CommandContext(ctx, binary, "--runtime-dir", dir)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if command.Start() != nil {
		t.Fatal("executable start failed")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = command.Process.Kill()
			<-done
		}
	})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("fixture trust failed")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ready := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(cfg.Origin + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if closeErr == nil && response.StatusCode == 200 {
				ready = true
				break
			}
		}
		select {
		case <-done:
			stopped = true
			t.Fatal("executable exited before readiness")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("certificate-verified readiness failed")
	}
	response, err := client.Get(cfg.Origin + "/signin")
	if err != nil {
		t.Fatal("actual signin request failed")
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != 200 || len(body) == 0 || response.TLS == nil || response.TLS.Version != tls.VersionTLS13 {
		t.Fatal("actual signin TLS response invalid")
	}
	exerciseAuthenticatedAssets(t, client, cfg.Origin, dir)
	wrong := &http.Client{Timeout: time.Second}
	if response, err := wrong.Get(cfg.Origin + "/readyz"); err == nil {
		_ = response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
	if command.Process.Signal(syscall.SIGTERM) != nil {
		t.Fatal("stop signal failed")
	}
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal("graceful executable stop failed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("graceful stop exceeded bound")
	}
	invalid := exec.CommandContext(ctx, binary, "--invalid", "CANARY_SECRET")
	if body, err := invalid.CombinedOutput(); err == nil || strings.Contains(string(body), "CANARY_SECRET") || !strings.Contains(string(body), "startup or serving unavailable") {
		t.Fatal("invalid executable invocation did not fail generically")
	}
	for _, material := range []string{cfg.DatabaseURL, cfg.MaterialKey, cfg.RateKey, string(key), dir} {
		if strings.Contains(output.String(), material) {
			t.Fatal("private configuration leaked")
		}
	}
}
func TestArgumentsAndAmbientSettings(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown", "CANARY_SECRET"}, {"--runtime-dir", ""}, {"--runtime-dir", "missing", "extra"}} {
		var output bytes.Buffer
		if run(context.Background(), args, &output) == nil || strings.Contains(output.String(), "CANARY_SECRET") {
			t.Fatal("invalid invocation accepted or echoed")
		}
	}
	t.Setenv("PGSERVICE", "CANARY_SECRET")
	var output bytes.Buffer
	if run(context.Background(), []string{"--runtime-dir", privateFiles(t)}, &output) == nil || output.Len() != 0 {
		t.Fatal("ambient PG configuration accepted")
	}
}

func exerciseAuthenticatedAssets(t *testing.T, client *http.Client, origin, directory string) {
	t.Helper()
	submit := func(path string, values url.Values) int {
		t.Helper()
		req, err := http.NewRequest("POST", origin+path, strings.NewReader(values.Encode()))
		if err != nil {
			t.Fatal("form construction failed")
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("form request failed")
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("form response failed")
		}
		return response.StatusCode
	}
	values := url.Values{"email": {"owner@example.test"}, "password": {"synthetic owner phrase for fixture 2026!"}}
	if submit("/signup", values) != 202 {
		t.Fatal("actual signup failed")
	}
	var action *url.URL
	deadline := time.Now().Add(5 * time.Second)
	for action == nil && time.Now().Before(deadline) {
		files, err := os.ReadDir(filepath.Join(directory, "mail"))
		if err != nil {
			t.Fatal("private capture unavailable")
		}
		if len(files) > 0 {
			body, err := os.ReadFile(filepath.Join(directory, "mail", files[0].Name()))
			if err != nil {
				t.Fatal("private capture unreadable")
			}
			var capture struct{ Text string }
			if json.Unmarshal(body, &capture) != nil {
				t.Fatal("private capture invalid")
			}
			first, _, _ := strings.Cut(capture.Text, "\n")
			action, err = url.Parse(first)
			if err != nil || action.Scheme != "https" || action.Host != strings.TrimPrefix(origin, "https://") {
				t.Fatal("private action invalid")
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if action == nil {
		t.Fatal("private verification capture missing")
	}
	if submit("/verify-email", url.Values{"challenge": {action.Query().Get("challenge")}, "token": {action.Query().Get("token")}}) != 200 {
		t.Fatal("actual verification failed")
	}
	if submit("/auth", values) != 303 {
		t.Fatal("actual signin failed")
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cookie := range client.Jar.Cookies(parsed) {
		if cookie.Name == "__Host-amos_session" {
			found = true
		}
	}
	if !found {
		t.Fatal("secure session absent")
	}
	get := func(path string) (int, string) {
		t.Helper()
		response, err := client.Get(origin + path)
		if err != nil {
			t.Fatal("page request failed")
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("page response failed")
		}
		return response.StatusCode, string(body)
	}
	status, page := get("/todos")
	if status != 200 || strings.Contains(page, "Workspace ID") {
		t.Fatal("personal workspace page unavailable")
	}
	match := regexp.MustCompile(`name="_csrf" value="([^"]+)"`).FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("session CSRF token absent")
	}
	if submit("/todos", url.Values{"title": {"must not persist"}}) != 403 {
		t.Fatal("CSRF denial missing")
	}
	if submit("/todos", url.Values{"_csrf": {match[1]}, "title": {"Owned executable todo"}}) != 303 {
		t.Fatal("actual personal todo mutation failed")
	}
	status, page = get("/todos")
	if status != 200 || !strings.Contains(page, "Owned executable todo") || strings.Contains(page, "must not persist") {
		t.Fatal("personal todo persistence invalid")
	}
	for path, kind := range map[string]string{"/reference/ui.css": "text/css", "/reference/htmx.min.js": "javascript", "/reference/csrf.js": "javascript"} {
		response, err := client.Get(origin + path)
		if err != nil {
			t.Fatal("authenticated asset request failed")
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != 200 || len(body) == 0 || !strings.Contains(response.Header.Get("Content-Type"), kind) {
			t.Fatal("authenticated reference asset unavailable")
		}
	}
	if submit("/signout", url.Values{"_csrf": {match[1]}}) != 204 {
		t.Fatal("actual signout failed")
	}
	if status, _ := get("/todos"); status != 401 {
		t.Fatal("revoked session retained access")
	}

}
