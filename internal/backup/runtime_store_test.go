package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/credentials/endpointcreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func runtimeTestConfig() ScheduleAWSConfig {
	return ScheduleAWSConfig{CredentialKind: "ecs_task", AccountID: "123456789012", Region: "us-east-1", RuntimeRoleARN: "arn:aws:iam::123456789012:role/amos-runtime-test", BucketAccountID: "123456789012", Bucket: "backup-test-fixture", Prefix: "installations/test", KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/test"}
}

type runtimeWireFixture struct {
	mu                 sync.Mutex
	mode               string
	expiry             time.Time
	metadata, identity int
	objects            []string
	payloads           [][]byte
	bad                bool
}

func (f *runtimeWireFixture) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.TLS == nil {
		f.bad = true
		w.WriteHeader(400)
		return
	}
	switch {
	case r.URL.Path == "/latest/api/token":
		if f.mode == "token-failure" {
			w.WriteHeader(403)
			return
		}
		if r.Method != "PUT" {
			f.bad = true
		}
		w.Header().Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
		_, _ = io.WriteString(w, "synthetic-imds-v2-token")
		return
	case r.URL.Path == "/latest/meta-data/iam/security-credentials/":
		if r.Header.Get("X-aws-ec2-metadata-token") != "synthetic-imds-v2-token" {
			f.bad = true
		}
		_, _ = io.WriteString(w, "amos-runtime-test")
		return
	case r.URL.Path == "/v2/credentials/test" || r.URL.Path == "/latest/meta-data/iam/security-credentials/amos-runtime-test":
		if strings.HasPrefix(r.URL.Path, "/latest/") && r.Header.Get("X-aws-ec2-metadata-token") != "synthetic-imds-v2-token" {
			f.bad = true
		}
		if f.mode == "slow-metadata" {
			<-r.Context().Done()
			return
		}
		f.metadata++
		key := "EXAMPLEKEYA"
		token := "synthetic-session-A"
		phrase := "dummy"
		if f.metadata > 1 {
			key = "EXAMPLEKEYB"
			token = "synthetic-session-B"
			phrase = "test"
		}
		body := map[string]any{"Code": "Success", "AccessKeyId": key, "SecretAccessKey": phrase, "Token": token, "Expiration": f.expiry.UTC().Format(time.RFC3339)}
		if f.mode == "oversized-metadata" {
			body["Padding"] = strings.Repeat("x", 65537)
		}
		_ = json.NewEncoder(w).Encode(body)
		return
	case strings.HasPrefix(r.URL.Path, "/sts"):
		f.identity++
		body, _ := io.ReadAll(io.LimitReader(r.Body, 65537))
		if f.mode == "slow-identity" {
			<-r.Context().Done()
			return
		}
		if !runtimeSignedBySnapshot(r, body, "sts") {
			f.bad = true
		}
		account := "123456789012"
		role := "amos-runtime-test"
		if f.mode == "wrong-account" {
			account = "999999999999"
		}
		if f.mode == "wrong-role" {
			role = "foreign-role"
		}
		identity := "arn:aws:sts::" + account + ":assumed-role/" + role + "/synthetic-session"
		if f.mode == "iam-user" {
			identity = "arn:aws:iam::123456789012:user/synthetic"
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprintf(w, `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Arn>%s</Arn><UserId>synthetic</UserId><Account>%s</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`, identity, account)
		return
	default:
		f.objects = append(f.objects, r.Method+" "+r.URL.Path)
		if r.Method != "PUT" {
			f.bad = true
			w.WriteHeader(403)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		f.payloads = append(f.payloads, append([]byte{}, body...))
		if f.mode == "slow-put" {
			<-r.Context().Done()
			return
		}
		sum := sha256.Sum256(body)
		checksum := base64.StdEncoding.EncodeToString(sum[:])
		if err != nil || !runtimeSignedBySnapshot(r, body, "s3") || r.Header.Get("X-Amz-Expected-Bucket-Owner") != "123456789012" || r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Server-Side-Encryption") != "aws:kms" || r.Header.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id") != runtimeTestConfig().KMSKeyARN || r.Header.Get("X-Amz-Checksum-Sha256") != checksum || r.ContentLength != int64(len(body)) {
			f.bad = true
		}
		if f.mode == "conflict" {
			w.WriteHeader(412)
			_, _ = io.WriteString(w, "<Error><Code>PreconditionFailed</Code></Error>")
			return
		}
		if f.mode == "partial" && strings.HasSuffix(r.URL.Path, "manifest.json") {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, "<Error><Code>InternalError</Code></Error>")
			return
		}
		if f.mode == "wrong-checksum" {
			checksum = base64.StdEncoding.EncodeToString(make([]byte, 32))
		}
		w.Header().Set("X-Amz-Checksum-Sha256", checksum)
		version := "synthetic-version"
		if f.mode == "null-version" {
			version = "null"
		}
		if f.mode != "missing-version" {
			w.Header().Set("X-Amz-Version-Id", version)
		}
		w.WriteHeader(200)
	}
}

// Verify the actual wire signature, including the captured secret, not just its
// public access-key label. Fixture paths contain no escaped URI components.
func runtimeSignedBySnapshot(r *http.Request, body []byte, service string) bool {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") || r.Header.Get("X-Amz-Security-Token") != "synthetic-session-A" {
		return false
	}
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256 "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return false
		}
		fields[key] = value
	}
	scopeParts := strings.Split(fields["Credential"], "/")
	if len(scopeParts) != 5 || scopeParts[0] != "EXAMPLEKEYA" || scopeParts[2] != "us-east-1" || scopeParts[3] != service || scopeParts[4] != "aws4_request" {
		return false
	}
	var headers strings.Builder
	for _, name := range strings.Split(fields["SignedHeaders"], ";") {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.Host
		}
		if name == "content-length" {
			value = strconv.FormatInt(r.ContentLength, 10)
		}
		headers.WriteString(name + ":" + strings.Join(strings.Fields(value), " ") + "\n")
	}
	sum := sha256.Sum256(body)
	payload := hex.EncodeToString(sum[:])
	if value := r.Header.Get("X-Amz-Content-Sha256"); value != "" {
		payload = value
	}
	canonical := strings.Join([]string{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, headers.String(), fields["SignedHeaders"], payload}, "\n")
	hash := sha256.Sum256([]byte(canonical))
	scope := strings.Join(scopeParts[1:], "/")
	toSign := "AWS4-HMAC-SHA256\n" + r.Header.Get("X-Amz-Date") + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	mac := func(key []byte, value string) []byte {
		h := hmac.New(sha256.New, key)
		_, _ = h.Write([]byte(value))
		return h.Sum(nil)
	}
	key := mac([]byte("AWS4dummy"), scopeParts[1])
	key = mac(key, scopeParts[2])
	key = mac(key, scopeParts[3])
	key = mac(key, scopeParts[4])
	return hmac.Equal([]byte(fields["Signature"]), []byte(hex.EncodeToString(mac(key, toSign))))
}
func runtimeFixtureClients(t *testing.T, kind, mode string) (*runtimeWireFixture, aws.CredentialsProvider, sts.Options, s3.Options) {
	t.Helper()
	fixture := &runtimeWireFixture{mode: mode, expiry: time.Now().Add(time.Hour)}
	if mode == "insufficient-expiry" {
		fixture.expiry = time.Now().Add(time.Second)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
	t.Cleanup(server.Close)
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	client := &http.Client{Transport: boundedMetadataTransport{transport: transport, timeout: 2 * time.Second}, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(transport.CloseIdleConnections)
	var provider aws.CredentialsProvider
	if kind == "imds_v2" {
		metadata := imds.New(imds.Options{Endpoint: server.URL, HTTPClient: client, ClientEnableState: imds.ClientEnabled, EnableFallback: aws.FalseTernary, Retryer: aws.NopRetryer{}})
		provider = ec2rolecreds.New(func(options *ec2rolecreds.Options) { options.Client = metadata })
	} else {
		provider = endpointcreds.New(server.URL+"/v2/credentials/test", func(options *endpointcreds.Options) { options.HTTPClient = client; options.Retryer = aws.NopRetryer{} })
	}
	return fixture, provider, sts.Options{BaseEndpoint: aws.String(server.URL + "/sts"), HTTPClient: client}, s3.Options{BaseEndpoint: aws.String(server.URL), UsePathStyle: true, HTTPClient: client}
}
func TestRuntimeStoreActualTLSMetadataIdentityAndPut(t *testing.T) {
	for _, kind := range []string{"ecs_task", "imds_v2"} {
		t.Run(kind, func(t *testing.T) {
			fixture, source, identity, objects := runtimeFixtureClients(t, kind, "")
			config := runtimeTestConfig()
			config.CredentialKind = kind
			store, err := newRuntimeStoreWithClients(context.Background(), config, 1<<20, time.Minute, source, identity, objects)
			if err != nil {
				t.Fatal("actual fixture identity preflight failed")
			}
			dir, manifest := makeStoreFixture(t, "runtime-test")
			if _, err := store.Upload(context.Background(), dir, manifest); err != nil {
				t.Fatal("actual SDK fixture publication failed")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.bad || fixture.metadata != 1 || fixture.identity != 1 || len(fixture.objects) != 2 || !strings.HasSuffix(fixture.objects[0], "database.dump") || !strings.HasSuffix(fixture.objects[1], "manifest.json") {
				t.Fatal("snapshot/signature/put-only/manifest-last wire invariant failed")
			}
			authority, ok := store.writer.(scheduleRuntimeAuthority)
			if !ok || authority.validateScheduleRuntime(config, time.Minute) != nil {
				t.Fatal("private runtime authority missing")
			}
			changed := config
			changed.Bucket = "different-test-bucket"
			if authority.validateScheduleRuntime(changed, time.Minute) == nil || authority.validateScheduleRuntime(config, 2*time.Hour) == nil {
				t.Fatal("runtime config/lifetime mismatch accepted")
			}
		})
	}
}

func TestRuntimeStoreIMDSMaximumLifetimeFailsClosed(t *testing.T) {
	fixture, source, identity, objects := runtimeFixtureClients(t, "imds_v2", "")
	// This pinned provider intentionally caps metadata credentials at one hour.
	// A full-hour run plus its publication allowance cannot use that snapshot.
	fixture.expiry = time.Now().Add(6 * time.Hour)
	config := runtimeTestConfig()
	config.CredentialKind = "imds_v2"
	if _, err := newRuntimeStoreWithClients(context.Background(), config, 1<<20, time.Hour+15*time.Second, source, identity, objects); err == nil {
		t.Fatal("IMDS snapshot lifetime cap was bypassed")
	}
	if fixture.identity != 0 || len(fixture.objects) != 0 {
		t.Fatal("insufficient snapshot contacted downstream services")
	}
}

func TestRuntimeStoreRejectsUntrustedServiceCertificates(t *testing.T) {
	for _, service := range []string{"identity", "objects"} {
		t.Run(service, func(t *testing.T) {
			fixture, source, identity, objects := runtimeFixtureClients(t, "ecs_task", "")
			transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: x509.NewCertPool()}}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: time.Second}
			if service == "identity" {
				identity.HTTPClient = client
			} else {
				objects.HTTPClient = client
			}
			store, err := newRuntimeStoreWithClients(context.Background(), runtimeTestConfig(), 1<<20, time.Minute, source, identity, objects)
			if service == "identity" {
				if err == nil {
					t.Fatal("untrusted STS certificate accepted")
				}
			} else {
				if err != nil {
					t.Fatal("trusted identity preflight failed")
				}
				dir, manifest := makeStoreFixture(t, "runtime-test")
				if _, err := store.Upload(context.Background(), dir, manifest); err == nil {
					t.Fatal("untrusted S3 certificate accepted")
				}
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if len(fixture.objects) != 0 {
				t.Fatal("untrusted service accepted an HTTP PUT")
			}
		})
	}
}
func TestRuntimeStoreRejectsPreflightAndAcknowledgementFailures(t *testing.T) {
	for _, mode := range []string{"wrong-account", "wrong-role", "iam-user", "insufficient-expiry", "oversized-metadata", "wrong-checksum", "null-version", "missing-version", "conflict", "partial", "token-failure"} {
		t.Run(mode, func(t *testing.T) {
			kind := "ecs_task"
			if mode == "token-failure" {
				kind = "imds_v2"
			}
			fixture, source, identity, objects := runtimeFixtureClients(t, kind, mode)
			config := runtimeTestConfig()
			config.CredentialKind = kind
			store, err := newRuntimeStoreWithClients(context.Background(), config, 1<<20, time.Minute, source, identity, objects)
			preflight := mode == "wrong-account" || mode == "wrong-role" || mode == "iam-user" || mode == "insufficient-expiry" || mode == "oversized-metadata" || mode == "token-failure"
			if preflight {
				if err == nil {
					t.Fatal("invalid runtime preflight accepted")
				}
			} else {
				if err != nil {
					t.Fatal("fixture preflight failed unexpectedly")
				}
				dir, manifest := makeStoreFixture(t, "runtime-failure")
				if _, err = store.Upload(context.Background(), dir, manifest); err == nil {
					t.Fatal("invalid/partial remote acknowledgement accepted")
				}
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if preflight && len(fixture.objects) != 0 || !preflight && len(fixture.objects) > 2 || fixture.metadata > 1 {
				t.Fatal("failed preflight retried/refreshed/published")
			}
			if fixture.bad {
				t.Fatal("wire identity/header/checksum mismatch")
			}
		})
	}
}
func TestRuntimeStoreEnvironmentAndRelativeEndpoint(t *testing.T) {
	for _, name := range []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_ROLE_ARN", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_ENDPOINT_URL_S3", "AWS_EC2_METADATA_SERVICE_ENDPOINT", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "")
			config := runtimeTestConfig()
			config.CredentialKind = "imds_v2"
			if _, err := NewRuntimeStore(context.Background(), config, 1<<20, time.Minute); err == nil {
				t.Fatal("ambient runtime override accepted")
			}
		})
	}
	for _, path := range []string{"https://foreign.test/x", "//foreign.test/x", "/v2/credentials/a/b", "/v2/credentials/..", "/v2/credentials/%61", "/v2/credentials/a?x=1", "/v2/credentials/"} {
		if runtimeRelativePath.MatchString(path) {
			t.Fatal("untrusted metadata endpoint accepted")
		}
	}
}

func TestRuntimeStoreCancellationAndActualExpiry(t *testing.T) {
	for _, mode := range []string{"slow-metadata", "slow-identity", "slow-put"} {
		t.Run(mode, func(t *testing.T) {
			fixture, source, identity, objects := runtimeFixtureClients(t, "ecs_task", mode)
			config := runtimeTestConfig()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			if mode != "slow-put" {
				if _, err := newRuntimeStoreWithClients(ctx, config, 1<<20, time.Minute, source, identity, objects); err == nil {
					t.Fatal("cancelled preflight accepted")
				}
			} else {
				store, err := newRuntimeStoreWithClients(context.Background(), config, 1<<20, time.Minute, source, identity, objects)
				if err != nil {
					t.Fatal("preflight unavailable")
				}
				dir, manifest := makeStoreFixture(t, "runtime-cancel")
				if _, err := store.Upload(ctx, dir, manifest); err == nil {
					t.Fatal("cancelled upload accepted")
				}
			}
			if time.Since(start) > time.Second {
				t.Fatal("bounded fixture cancellation exceeded one second")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.metadata > 1 || len(fixture.objects) > 1 {
				t.Fatal("cancelled runtime retried/refreshed")
			}
		})
	}
	t.Run("expiry-after-preflight", func(t *testing.T) {
		fixture, source, identity, objects := runtimeFixtureClients(t, "ecs_task", "")
		fixture.expiry = time.Now().Add(1500 * time.Millisecond)
		config := runtimeTestConfig()
		store, err := newRuntimeStoreWithClients(context.Background(), config, 1<<20, 10*time.Millisecond, source, identity, objects)
		if err != nil {
			t.Fatal("short-lived valid preflight failed")
		}
		writer := store.writer.(*runtimeWriter)
		time.Sleep(time.Until(writer.expires) + 5*time.Millisecond)
		dir, manifest := makeStoreFixture(t, "runtime-expired")
		if _, err := store.Upload(context.Background(), dir, manifest); err == nil {
			t.Fatal("expired snapshot published")
		}
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if fixture.metadata != 1 || len(fixture.objects) != 0 {
			t.Fatal("expired credentials refreshed or used")
		}
	})
}
