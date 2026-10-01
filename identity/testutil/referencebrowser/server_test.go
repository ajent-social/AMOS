// Package referencebrowser contains only an explicit browser test fixture.
// It seeds synthetic active identities, but uses actual session authorization,
// PostgreSQL persistence and reference UI. It does not qualify signup/providers.
package referencebrowser

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/reference/app/business"
	referenceui "github.com/ajent-social/amos/examples/reference/ui"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestServeReferenceBrowser(t *testing.T) {
	if os.Getenv("AMOS_REFERENCE_BROWSER_SERVE") != "1" {
		t.Skip("explicit browser fixture only; AMOS_REFERENCE_BROWSER_SERVE=1 starts the required-service host")
	}
	port := 4187
	if value := os.Getenv("AMOS_REFERENCE_BROWSER_PORT"); value != "" {
		var err error
		port, err = strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			t.Fatal("invalid browser fixture port")
		}
	}
	_, schema := testkit.NewPostgres(t)
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := storage.Open(context.Background(), parsed.String())
	if err != nil {
		t.Fatal("open isolated browser database")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	root := os.Getenv("AMOS_REFERENCE_BROWSER_ROOT")
	if root == "" {
		var e error
		root, e = os.Getwd()
		if e != nil {
			t.Fatal(e)
		}
	}
	fragments := []migrations.Fragment{}
	for index, spec := range []struct{ namespace, path string }{{"identity", "migrations/fragments/identity.sql"}, {"workspace", "migrations/fragments/workspace.sql"}, {"reference", "examples/reference/migrations/todos.sql"}} {
		data, err := os.ReadFile(filepath.Join(root, spec.path))
		if err != nil {
			t.Fatal(err)
		}
		fragments = append(fragments, migrations.Fragment{Namespace: spec.namespace, Migrations: []migrations.Migration{{Sequence: uint64(index + 1), Name: "base", SQL: string(data)}}})
	}
	registry, err := migrations.NewRegistry(fragments...)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Migrate(context.Background(), db, registry); err != nil {
		t.Fatal(err)
	}
	id := func() uuid.UUID {
		v, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	installation, application, environment := id(), id(), id()
	scope := workspacestore.Scope{InstallationID: installation, ApplicationID: application}
	people := map[string]uuid.UUID{"alice": id(), "bob": id()}
	workspaces := map[string]uuid.UUID{}
	if err = db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		ws, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		for name, person := range people {
			if _, err = tx.Exec(`INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, person, installation, application); err != nil {
				return err
			}
			workspace, err := ws.CreatePersonalWorkspace(context.Background(), workspacestore.CreatePersonalInput{ID: id(), Scope: scope, OwnerPersonID: person})
			if err != nil {
				return err
			}
			workspaces[name] = workspace.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	origin := "http://127.0.0.1:" + strconv.Itoa(port)
	sessions, err := session.New(db, session.Config{InstallationID: installation, ApplicationID: application, EnvironmentID: environment, AllowedOrigins: []string{origin}, DevelopmentLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	todos, err := business.New(db)
	if err != nil {
		t.Fatal(err)
	}
	// Integrator attaches the UI's request-bound CSRF resolver once its reviewed
	// option is present; no protected form may fabricate a token.
	ui := referenceui.NewHandler(todos, referenceui.Options{CSRFToken: sessions.CSRFToken})
	protected := sessions.Middleware(ui)
	mux := http.NewServeMux()
	mux.Handle("/todos", protected)
	mux.Handle("/todos/", protected)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /_test/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"workspace": workspaces["alice"].String(), "foreignWorkspace": workspaces["bob"].String()}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("POST /_test/login", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid fixture input", 400)
			return
		}
		name := r.PostForm.Get("account")
		person, ok := people[name]
		if !ok {
			http.Error(w, "unknown synthetic fixture", 400)
			return
		}
		now := time.Now().UTC()
		proof, err := authproof.NewVerifiedCredential(person, installation, application, environment, 0, "email_password", now, "aal1", now.Add(time.Hour))
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		issued, err := sessions.IssueForRequest(r.Context(), proof, r)
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		http.SetCookie(w, issued.Cookie)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"workspace": workspaces[name].String(), "csrfToken": issued.CSRFToken}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("POST /_test/reset", func(w http.ResponseWriter, r *http.Request) {
		if err := db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(r.Context(), "DELETE FROM reference_todos")
			return err
		}); err != nil {
			http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(204)
	})
	mux.Handle("/", ui)
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
	select {
	case err := <-ended:
		if err != nil && err != http.ErrServerClosed {
			t.Fatal(err)
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		t.Error(err)
	}
}
