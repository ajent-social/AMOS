package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// This component uses synthetic account/session setup followed by actual native
// middleware. It does not claim a complete signup/sign-in-to-business journey.
type privateReadTestFixture struct {
	core                             *readCore
	ctx                              context.Context
	cfg                              coreConfig
	token                            string
	sourceID, foreignID, workspaceID uuid.UUID
}

// privateReadFixture shares exact synthetic setup between distinct read and HTTP
// suites. Each caller runs in its own process because W1 selection is immutable.
func privateReadFixture(t *testing.T) *privateReadTestFixture {
	t.Helper()
	path := os.Getenv("AMOS_CONTINUITY_READ_RUNTIME_TEST_CONFIG")
	if path == "" {
		t.Fatal("required same-database W1/continuity TLS fixture absent")
	}
	if os.Getenv("AMOS_CONTINUITY_READ_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
		cmd.Env = append(os.Environ(), "AMOS_CONTINUITY_READ_CHILD=1")
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("private reader child failed: %v\n%s", e, output)
		}
		return nil
	}
	var input struct {
		Host           string `json:"host"`
		Port           uint16 `json:"port"`
		Database       string `json:"database"`
		User           string `json:"user"`
		Password       string `json:"password"`
		CAPath         string `json:"ca_path"`
		WrongHost      string `json:"wrong_host"`
		DMLTable       string `json:"dml_table"`
		LedgerTable    string `json:"ledger_table"`
		PrivilegedRole string `json:"privileged_role"`
		OwnerRole      string `json:"owner_role"`
	}
	file, e := os.Open(path)
	if e != nil {
		t.Fatal("required fixture config unavailable")
	}
	dec := json.NewDecoder(io.LimitReader(file, 1<<20))
	dec.DisallowUnknownFields()
	e = dec.Decode(&input)
	closeErr := file.Close()
	if e != nil || closeErr != nil {
		t.Fatal("required fixture config invalid")
	}
	roots, e := os.ReadFile(input.CAPath)
	if e != nil {
		t.Fatal("required fixture CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	cfg := coreConfig{Database: storage.RuntimeConfig{Host: input.Host, Port: input.Port, Database: input.Database, User: input.User, Password: input.Password, RootCAPEM: roots, StartupTimeout: 3 * time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}, InstallationID: testID(t), ApplicationID: testID(t), EnvironmentID: testID(t), Origin: "https://reader.example.test"}
	core, e := openReadCore(ctx, cfg)
	if e != nil {
		t.Fatal("required private runtime composition unavailable")
	}
	t.Cleanup(func() {
		if e := core.close(); e != nil {
			t.Error("private runtime close failed")
		}
	})
	person, workspace, sessionID, sourceID, foreignID := testID(t), testID(t), testID(t), testID(t), testID(t)
	raw := bytes.Repeat([]byte{0x49}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	body := "Literal 50%_ source <script>untrusted</script>"
	bodyHash := sha256.Sum256([]byte(body))
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := core.database.WithTx(c, nil, func(tx *sql.Tx) error {
			for _, q := range []string{`DELETE FROM continuity_sources WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_sessions WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM workspaces WHERE installation_id=$1 AND application_id=$2`, `DELETE FROM identity_persons WHERE installation_id=$1 AND application_id=$2`} {
				if _, e := tx.ExecContext(c, q, cfg.InstallationID, cfg.ApplicationID); e != nil {
					return e
				}
			}
			return nil
		}); e != nil {
			t.Error("exact-owned private reader cleanup failed")
		}
	})
	if e := core.database.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, cfg.InstallationID, cfg.ApplicationID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO workspaces(id,installation_id,application_id,kind,state,personal_owner_id) VALUES($1,$2,$3,'personal','active',$4)`, workspace, cfg.InstallationID, cfg.ApplicationID, person); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,issued_at,authenticated_at,last_seen_at,expires_at,idle_expires_at) SELECT $1,$2,$3,$4,$5,$6,0,'email_password',at,at,at,at+interval '1 hour',at+interval '30 minutes' FROM(SELECT clock_timestamp() AS at) sample`, sessionID, person, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, tokenHash[:]); e != nil {
			return e
		}
		for _, row := range []struct{ id, workspace string }{{sourceID.String(), workspace.String()}, {foreignID.String(), testID(t).String()}} {
			if _, e := tx.ExecContext(ctx, `INSERT INTO continuity_sources(installation_id,application_id,environment_id,workspace_id,id,kind,title,body,sha256) VALUES($1,$2,$3,$4,$5,'document','Reference source',$6,$7)`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID, row.workspace, row.id, body, hex.EncodeToString(bodyHash[:])); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal("required source fixture tables or coherent setup unavailable")
	}
	return &privateReadTestFixture{core: core, ctx: ctx, cfg: cfg, token: token, sourceID: sourceID, foreignID: foreignID, workspaceID: workspace}
}

func TestPrivateReadRuntimeRequiredService(t *testing.T) {
	f := privateReadFixture(t)
	if f == nil {
		return
	}
	core, ctx, cfg, token, sourceID, foreignID := f.core, f.ctx, f.cfg, f.token, f.sourceID, f.foreignID
	read := func(t *testing.T, q sourceQuery) ([]byte, error) {
		t.Helper()
		var result []byte
		var resultErr error
		called := false
		h := core.sessions.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			result, resultErr = core.source(r.Context(), q)
		}))
		r := httptest.NewRequest(http.MethodGet, cfg.Origin+"/continuity/sources", nil).WithContext(ctx)
		r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if !called {
			t.Fatalf("native reader admission status=%d", w.Code)
		}
		return result, resultErr
	}
	t.Run("ordinary current personal literal search", func(t *testing.T) {
		b, e := read(t, sourceQuery{Query: "50%_", Limit: 10})
		if e != nil || !bytes.Contains(b, []byte(sourceID.String())) || bytes.Contains(b, []byte(foreignID.String())) {
			t.Fatal("scoped literal source retrieval failed")
		}
	})
	t.Run("detail escaped and digest verified", func(t *testing.T) {
		b, e := read(t, sourceQuery{ID: sourceID.String()})
		if e != nil || !strings.Contains(string(b), "&lt;script&gt;") || strings.Contains(string(b), "<script>untrusted") {
			t.Fatal("detail source rendering failed")
		}
	})
	t.Run("foreign detail is not found with no bytes", func(t *testing.T) {
		b, e := read(t, sourceQuery{ID: foreignID.String()})
		if b != nil || !errors.Is(e, errNotFound) {
			t.Fatal("foreign source disclosed")
		}
	})
	t.Run("context without private admission has no authority", func(t *testing.T) {
		b, e := core.source(ctx, sourceQuery{Limit: 10})
		if b != nil || !errors.Is(e, errDenied) {
			t.Fatal("unadmitted context disclosed")
		}
	})
	t.Run("canceled original request has no bytes", func(t *testing.T) {
		c, stop := context.WithCancel(ctx)
		stop()
		b, e := core.source(c, sourceQuery{Limit: 10})
		if b != nil || !errors.Is(e, errUnavailable) {
			t.Fatal("canceled request disclosed")
		}
	})
}
