package telemetryconsumer_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracewire "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const tracingCanary = "SYNTHETIC_PRIVATE_SQL_BODY_CREDENTIAL_51"
const tracingToken = "dummy"

type tracingCA struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func newTracingCA(t *testing.T) *tracingCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic tracing test authority"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &tracingCA{certificate: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}
func (ca *tracingCA) identity(t *testing.T, name string, client bool) (tls.Certificate, []byte, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageServerAuth
	if client {
		usage = x509.ExtKeyUsageClientAuth
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	if ip := net.ParseIP(name); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	return pair, certPEM, keyPEM, digest[:]
}
func serveTracingTLS(t *testing.T, certificate tls.Certificate, clientCA []byte, handler http.Handler) (string, *http.Server) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}}
	if clientCA != nil {
		cfg.ClientCAs = x509.NewCertPool()
		if !cfg.ClientCAs.AppendCertsFromPEM(clientCA) {
			t.Fatal("client trust fixture invalid")
		}
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, TLSConfig: cfg}
	done := make(chan error, 1)
	go func() { done <- server.Serve(tls.NewListener(listener, cfg)) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if server.Shutdown(ctx) != nil {
			_ = server.Close()
		}
		err := <-done
		if err != nil && err != http.ErrServerClosed {
			t.Error("TLS fixture serving failed")
		}
	})
	return listener.Addr().String(), server
}

type tracingCollector struct {
	mu       sync.Mutex
	requests []*collector.ExportTraceServiceRequest
	invalid  bool
}

func (sink *tracingCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	var request collector.ExportTraceServiceRequest
	valid := err == nil && len(body) <= 65536 && r.Method == "POST" && r.URL.Path == "/v1/traces" && r.URL.RawQuery == "" && r.ProtoMajor == 1 && r.ProtoMinor == 1 && r.TLS != nil && r.TLS.Version == tls.VersionTLS13 && r.Header.Get("Authorization") == "Bearer "+tracingToken && r.Header.Get("Content-Type") == "application/x-protobuf" && (r.Header.Get("Content-Encoding") == "" || r.Header.Get("Content-Encoding") == "identity") && proto.Unmarshal(body, &request) == nil
	sink.mu.Lock()
	if valid {
		sink.requests = append(sink.requests, proto.Clone(&request).(*collector.ExportTraceServiceRequest))
	} else {
		sink.invalid = true
	}
	sink.mu.Unlock()
	if !valid {
		http.Error(w, "invalid fixture export", 400)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(200)
}
func (sink *tracingCollector) snapshot() ([]*collector.ExportTraceServiceRequest, bool) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	result := make([]*collector.ExportTraceServiceRequest, len(sink.requests))
	for i, request := range sink.requests {
		result[i] = proto.Clone(request).(*collector.ExportTraceServiceRequest)
	}
	return result, sink.invalid
}
func (sink *tracingCollector) spans(t *testing.T, want int) []*tracewire.Span {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		requests, invalid := sink.snapshot()
		if invalid {
			t.Fatal("collector received invalid protocol/authentication")
		}
		var spans []*tracewire.Span
		for _, request := range requests {
			for _, resource := range request.ResourceSpans {
				for _, scope := range resource.ScopeSpans {
					spans = append(spans, scope.Spans...)
				}
			}
		}
		if len(spans) >= want {
			return spans
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("required real collector spans missing")
	return nil
}

type tracingDatabase struct {
	runner storage.TxRunner
	table  string
}

func plainTracingDatabase(t *testing.T) tracingDatabase {
	t.Helper()
	raw := os.Getenv("AMOS_TEST_DATABASE_URL")
	if raw == "" {
		t.Fatal("required PostgreSQL fixture unavailable")
	}
	pool, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal("PostgreSQL fixture unavailable")
	}
	schema := "tracing_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("isolated tracing schema unavailable")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("owned tracing schema cleanup failed")
		}
		if pool.Close() != nil {
			t.Error("fixture pool close failed")
		}
	})
	if _, err := pool.ExecContext(ctx, "CREATE TABLE "+schema+".records(id uuid PRIMARY KEY,value text NOT NULL)"); err != nil {
		t.Fatal("isolated tracing table unavailable")
	}
	db, err := storage.Open(ctx, raw)
	if err != nil {
		t.Fatal("development tracing handle unavailable")
	}
	t.Cleanup(func() {
		if db.Close() != nil {
			t.Error("development handle close failed")
		}
	})
	return tracingDatabase{runner: db, table: schema + ".records"}
}
func runtimeTracingDatabase(t *testing.T) tracingDatabase {
	t.Helper()
	path := os.Getenv("AMOS_TRACING_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required actual TLS restricted-role tracing profile unavailable")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("runtime tracing profile unavailable")
	}
	var config struct {
		Host           string `json:"host"`
		Port           uint16 `json:"port"`
		Database       string `json:"database"`
		User           string `json:"user"`
		Password       string `json:"password"`
		CAPath         string `json:"ca_path"`
		DMLTable       string `json:"dml_table"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
		WrongHost      string `json:"wrong_host"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&config) != nil || dec.Decode(new(any)) != io.EOF || !regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}\.[a-z_][a-z0-9_]{0,62}$`).MatchString(config.DMLTable) {
		t.Fatal("runtime tracing profile invalid")
	}
	roots, err := os.ReadFile(config.CAPath)
	if err != nil {
		t.Fatal("runtime tracing roots unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := storage.OpenRuntime(ctx, storage.RuntimeConfig{Host: config.Host, Port: config.Port, Database: config.Database, User: config.User, Password: config.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal("actual TLS restricted runtime connection failed")
	}
	t.Cleanup(func() {
		if db.Close() != nil {
			t.Error("runtime tracing handle close failed")
		}
	})
	return tracingDatabase{runner: db, table: config.DMLTable}
}
func tracingAuthority(address, name string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		panic("invalid synthetic listener")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number == 0 {
		panic("invalid synthetic port")
	}
	return net.JoinHostPort(name, port)
}
