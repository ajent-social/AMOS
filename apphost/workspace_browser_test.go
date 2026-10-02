package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
	workspacestore "github.com/ajent-social/amos/workspace/store"
	"github.com/google/uuid"
)

func TestWorkspaceSwitchBrowserUsesCurrentMembership(t *testing.T) {
	workspaceBrowser := os.Getenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")
	if workspaceBrowser == "" {
		t.Fatal("required PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH is absent; point it to the installed Google Chrome executable")
	}
	playwrightCLI := filepath.Join("..", "tests", "browser", "node_modules", "@playwright", "test", "cli.js")
	if _, err := os.Stat(workspaceBrowser); err != nil {
		t.Fatalf("required Chrome executable missing at the configured system location: %v", err)
	}
	if _, err := os.Stat(playwrightCLI); err != nil {
		t.Fatalf("pinned Playwright dependency missing; install tests/browser dependencies before this required browser check: %v", err)
	}

	dsn := localDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("start local workspace host")
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	cfg := localConfig(t, dsn, origin)
	revokeToken := uuid.NewString()
	// The browser fixture IDs are assigned only after the real account signs in.
	var browserPersonID, browserWorkspaceA, browserWorkspaceB uuid.UUID
	cfg.Business = func(db *storage.DB, sessions *session.Service) ([]Route, error) {
		resolver, err := workspacecontext.New(db, workspacecontext.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID})
		if err != nil {
			return nil, err
		}
		resource := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			selection, ok := workspacecontext.FromContext(r.Context())
			if !ok {
				http.Error(w, "selection missing", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store, max-age=0")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, "<!doctype html><html><body><header>Current workspace: %s</header><main>Synthetic record for %s</main></body></html>", selectionLabel(selection.Workspace.ID), selection.Workspace.ID)
		})
		revoke := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.Header.Get("X-Test-Authorization") != revokeToken {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err := db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
				store, err := workspacestore.New(tx)
				if err != nil {
					return err
				}
				_, err = store.UpdateMembership(r.Context(), workspacestore.UpdateMembershipInput{Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, WorkspaceID: browserWorkspaceB, PersonID: browserPersonID, Role: workspacestore.RoleMember, RoleVersion: 1, State: workspacestore.MembershipLeft})
				return err
			}); err != nil {
				http.Error(w, "membership revoke failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		return []Route{
			{Method: http.MethodGet, Pattern: "/private/data", Handler: sessions.Middleware(resolver.Middleware(resource))},
			{Method: http.MethodPost, Pattern: "/__test/revoke-b", Handler: revoke},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := NewLocal(ctx, cfg)
	if err != nil {
		t.Fatalf("compose local apphost with current-state workspace service: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case serveErr := <-done:
			if serveErr != nil {
				t.Error(serveErr)
			}
		case <-time.After(5 * time.Second):
			_ = listener.Close()
			t.Error("workspace apphost failed to stop")
		}
		if closeErr := host.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("create browser bootstrap cookie jar")
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	email := "workspace-owner@example.test"
	generatedTestPassword := uuid.NewString() + "Workspace!"
	status, _ := response(t, client, "POST", origin+"/signup", origin, url.Values{"email": {email}, "password": {generatedTestPassword}})
	if status != http.StatusAccepted {
		t.Fatalf("real signup status=%d", status)
	}
	verifyURL := localVerificationURL(t, cfg.MailDirectory)
	status, _ = response(t, client, http.MethodGet, verifyURL, "", nil)
	if status != http.StatusOK {
		t.Fatalf("verification preview status=%d", status)
	}
	verify, err := url.Parse(verifyURL)
	if err != nil {
		t.Fatal("parse captured verification action")
	}
	status, _ = response(t, client, http.MethodPost, origin+"/verify-email", origin, url.Values{"challenge": {verify.Query().Get("challenge")}, "token": {verify.Query().Get("token")}})
	if status != http.StatusOK {
		t.Fatalf("verification completion status=%d", status)
	}
	status, _ = response(t, client, http.MethodPost, origin+"/auth", origin, url.Values{"email": {email}, "password": {generatedTestPassword}})
	if status != http.StatusSeeOther {
		t.Fatalf("real sign-in status=%d", status)
	}

	workspaceAID, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate first organization identifier")
	}
	workspaceBID, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate second organization identifier")
	}
	ownerMembershipA, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate first owner membership identifier")
	}
	ownerMembershipB, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate second owner membership identifier")
	}
	backupOwner, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate backup owner identifier")
	}
	backupMembership, err := uuid.NewV7()
	if err != nil {
		t.Fatal("generate backup membership identifier")
	}
	if err := host.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT p.id FROM identity_persons p JOIN identity_emails e ON e.person_id=p.id WHERE p.installation_id=$1 AND p.application_id=$2 AND e.comparison_key=$3 AND p.state='active'`, cfg.InstallationID, cfg.ApplicationID, strings.ToLower(email)).Scan(&browserPersonID); err != nil {
			return err
		}
		store, err := workspacestore.New(tx)
		if err != nil {
			return err
		}
		browserWorkspaceA = workspaceAID
		_, _, err = store.CreateOrganizationWorkspace(ctx, workspacestore.CreateOrganizationInput{ID: browserWorkspaceA, OwnerMembershipID: ownerMembershipA, Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, OwnerPersonID: browserPersonID})
		if err != nil {
			return err
		}
		browserWorkspaceB = workspaceBID
		_, _, err = store.CreateOrganizationWorkspace(ctx, workspacestore.CreateOrganizationInput{ID: browserWorkspaceB, OwnerMembershipID: ownerMembershipB, Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, OwnerPersonID: browserPersonID})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, backupOwner, cfg.InstallationID, cfg.ApplicationID); err != nil {
			return err
		}
		_, err = store.AddMembership(ctx, workspacestore.AddMembershipInput{ID: backupMembership, Scope: workspacestore.Scope{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID}, WorkspaceID: browserWorkspaceB, PersonID: backupOwner, Role: workspacestore.RoleOwner, RoleVersion: 1})
		return err
	}); err != nil {
		t.Fatal("seed scoped organization memberships", err)
	}

	urlOrigin, err := url.Parse(origin)
	if err != nil {
		t.Fatal("parse local app origin")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range jar.Cookies(urlOrigin) {
		if cookie.Name == "amos_dev_session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("real sign-in session cookie missing")
	}

	if os.Getenv("TMPDIR") == "" {
		t.Fatal("TMPDIR must point to the task's disposable external artifact directory")
	}
	artifacts, err := os.MkdirTemp(os.TempDir(), "amos-workspace-browser-")
	if err != nil {
		t.Fatal("create external temporary browser artifacts")
	}
	t.Cleanup(func() { _ = os.RemoveAll(artifacts) })
	commandCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
	defer stop()
	command := exec.CommandContext(commandCtx, "node", "node_modules/@playwright/test/cli.js", "test", "--config=workspace.playwright.config.ts", "workspace-switch.spec.ts")
	command.Dir = filepath.Join("..", "tests", "browser")
	command.Env = append(os.Environ(),
		"AMOS_WORKSPACE_TEST_BASE_URL="+origin,
		"AMOS_WORKSPACE_SESSION_COOKIE_NAME="+sessionCookie.Name,
		"AMOS_WORKSPACE_SESSION_COOKIE_VALUE="+sessionCookie.Value,
		"AMOS_WORKSPACE_A_ID="+browserWorkspaceA.String(),
		"AMOS_WORKSPACE_B_ID="+browserWorkspaceB.String(),
		"AMOS_WORKSPACE_A_NAME="+selectionLabel(browserWorkspaceA),
		"AMOS_WORKSPACE_B_NAME="+selectionLabel(browserWorkspaceB),
		"AMOS_WORKSPACE_REVOKE_TOKEN="+revokeToken,
		"AMOS_WORKSPACE_UNAUTHORIZED_ID="+mustUUID(t).String(),
		"AMOS_WORKSPACE_PLAYWRIGHT_OUTPUT_DIR="+filepath.Join(artifacts, "results"),
		"PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH="+workspaceBrowser,
		"TMPDIR="+artifacts,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("required real-Chrome workspace browser check failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func localVerificationURL(t *testing.T, directory string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		files, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal("read local verification capture")
		}
		for _, file := range files {
			data, err := os.ReadFile(filepath.Join(directory, file.Name()))
			if err != nil {
				t.Fatal("read local verification capture")
			}
			var capture struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(data, &capture) != nil {
				continue
			}
			first, _, _ := strings.Cut(capture.Text, "\n")
			if strings.Contains(first, "/verify-email") {
				return first
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("real verification email was not captured")
	return ""
}

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func selectionLabel(id uuid.UUID) string { return "Organization " + id.String()[28:] }
