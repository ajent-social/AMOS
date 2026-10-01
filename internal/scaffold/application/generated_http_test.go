package application

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/testkit"
)

// This is a real generated native application, isolated PostgreSQL schema and
// Chromium lifecycle. The private mailbox is local capture, not provider proof.
func TestGeneratedNativeApplicationPasswordBrowserLifecycle(t *testing.T) {
	_, schema := testkit.NewPostgres(t)
	config, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(config)
	if err != nil {
		t.Fatal("invalid required database configuration")
	}
	query := dsn.Query()
	query.Set("search_path", schema)
	dsn.RawQuery = query.Encode()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	browser := filepath.Join(root, "tests/browser/node_modules/.bin/playwright")
	if _, err := os.Stat(browser); err != nil {
		t.Fatal("pinned Playwright installation is required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	input := initializer.Input{SchemaVersion: 1, AppSlug: "native-todo", ModulePath: "example.test/native/todo", ParentDir: parent, Target: "app", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: origin}
	result, err := initializer.Initialize(context.Background(), input, Generator{SourceDir: root})
	if err != nil {
		t.Fatal("generate native application", err)
	}
	project := filepath.Join(parent, result.Target)
	native := filepath.Join(parent, "native-app")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", native, "./cmd/app")
	build.Dir = project
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated consumer build failed: %v\n%s", err, output)
	}
	migration := exec.CommandContext(ctx, native, "migrate")
	migration.Dir = project
	migration.Env = fixtureEnvironment(os.Environ(), map[string]string{"AMOS_MIGRATION_DATABASE_URL": dsn.String()})
	if output, err := migration.CombinedOutput(); err != nil {
		t.Fatalf("generated native migration failed: %v\n%s", err, output)
	}
	serving, cancelServe := context.WithCancel(ctx)
	command := exec.CommandContext(serving, native, "serve")
	command.Dir = project
	command.Env = fixtureEnvironment(os.Environ(), map[string]string{"AMOS_RUNTIME_DATABASE_URL": dsn.String()})
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal("start generated native application")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait(); close(done) }()
	t.Cleanup(func() {
		cancelServe()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("native application failed to stop")
		}
	})
	client := &http.Client{Timeout: time.Second}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case <-ctx.Done():
			t.Fatal("generated application readiness timed out")
		case <-done:
			t.Fatal("generated native application exited before readiness")
		case <-ticker.C:
			response, err := client.Get(origin + "/readyz")
			if err != nil {
				continue
			}
			ready = response.StatusCode == http.StatusOK
			if err := response.Body.Close(); err != nil {
				t.Fatal("close readiness response")
			}
		}
	}
	artifacts := filepath.Join(parent, "browser-output")
	if err := os.Mkdir(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	browserCommand := exec.CommandContext(ctx, browser, "test", "--config", filepath.Join(root, "tests/browser/auth-password.playwright.config.ts"), "--project=chromium")
	browserCommand.Dir = filepath.Join(root, "tests/browser")
	browserCommand.Env = append(os.Environ(), "AMOS_AUTH_BASE_URL="+origin, "AMOS_AUTH_MAIL_DIRECTORY="+filepath.Join(project, ".amos/mail"), "AMOS_BROWSER_ARTIFACT_DIRECTORY="+artifacts)
	if output, err := browserCommand.CombinedOutput(); err != nil {
		t.Fatalf("generated app browser lifecycle failed: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
}

func fixtureEnvironment(base []string, values map[string]string) []string {
	result := append([]string(nil), base...)
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
