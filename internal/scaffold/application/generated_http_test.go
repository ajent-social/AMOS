package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/devrunner"
	"github.com/ajent-social/amos/internal/initializer"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/google/uuid"
)

// This is a real generated native application, isolated PostgreSQL schema and
// Chromium lifecycle. The private mailbox is local capture, not provider proof.
func TestGeneratedNativeApplicationPasswordBrowserLifecycle(t *testing.T) {
	testGeneratedPasswordBrowser(t, false)
}
func TestGeneratedNativeRunnerApplicationPasswordBrowserLifecycle(t *testing.T) {
	testGeneratedPasswordBrowser(t, true)
}
func testGeneratedPasswordBrowser(t *testing.T, useRunner bool) {
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
	parent := ownerTempDir(t)
	slug := "native-todo"
	if useRunner {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		slug = "native-" + id.String()[24:]
	}
	input := initializer.Input{SchemaVersion: 1, AppSlug: slug, ModulePath: "example.test/native/todo", ParentDir: parent, Target: "app", Modules: []string{"identity", "workspace"}, Mode: "evaluation", BusinessMode: "integrated-go", PublicOrigin: origin}
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
	serving, cancelServe := context.WithCancel(ctx)
	done := make(chan error, 1)
	var runnerLogs bytes.Buffer
	if useRunner {
		portListener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := strconv.Itoa(portListener.Addr().(*net.TCPAddr).Port)
		if err := portListener.Close(); err != nil {
			t.Fatal(err)
		}
		envPath := filepath.Join(project, ".env.local")
		env, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal("private local environment unavailable")
		}
		env = []byte(strings.Replace(string(env), "AMOS_DB_PORT=55432", "AMOS_DB_PORT="+port, 1))
		if err := os.WriteFile(envPath, env, 0600); err != nil {
			t.Fatal("private local environment update failed")
		}
		podman, err := exec.LookPath("podman")
		if err != nil {
			t.Fatal("required Podman prerequisite unavailable")
		}
		// The pinned PostgreSQL image is public. Use an owned anonymous registry
		// configuration for this fixture without changing operator credentials.
		authFile := filepath.Join(parent, "public-registry-auth.json")
		if err := os.WriteFile(authFile, []byte("{}"), 0600); err != nil {
			t.Fatal("private registry fixture unavailable")
		}
		t.Setenv("REGISTRY_AUTH_FILE", authFile)
		go func() {
			done <- devrunner.Run(serving, devrunner.Options{Project: slug, WorkingDir: project, AppBinary: native, ReadinessURL: origin + "/readyz", PodmanBinary: podman, Output: &runnerLogs})
			close(done)
		}()
		t.Cleanup(func() {
			identity, err := devrunner.InitializeProject(context.Background(), project, slug)
			if err != nil {
				t.Error("fixture project identity unavailable")
				return
			}
			out, err := exec.Command(podman, "volume", "inspect", "--format", "{{json .Labels}}", slug+"-postgres-data").Output()
			if err != nil {
				t.Error("fixture retained volume unavailable")
				return
			}
			var labels map[string]string
			if json.Unmarshal(out, &labels) != nil || labels["io.amos.project"] != slug || labels["io.amos.project_id"] != identity.ID.String() || labels["io.amos.resource"] != "database-volume" {
				t.Error("fixture volume ownership mismatch; retained")
				return
			}
			if err := exec.Command(podman, "volume", "rm", slug+"-postgres-data").Run(); err != nil {
				t.Error("remove owned fixture volume failed")
			}
		})
	} else {
		migration := exec.CommandContext(ctx, native, "migrate")
		migration.Dir = project
		migration.Env = fixtureEnvironment(os.Environ(), map[string]string{"AMOS_MIGRATION_DATABASE_URL": dsn.String()})
		if output, err := migration.CombinedOutput(); err != nil {
			t.Fatalf("generated native migration failed: %v\n%s", err, output)
		}
		command := exec.CommandContext(serving, native, "serve")
		command.Dir = project
		command.Env = fixtureEnvironment(os.Environ(), map[string]string{"AMOS_RUNTIME_DATABASE_URL": dsn.String()})
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		if err := command.Start(); err != nil {
			t.Fatal("start generated native application")
		}
		go func() { done <- command.Wait(); close(done) }()
	}
	t.Cleanup(func() {
		cancelServe()
		select {
		case err := <-done:
			if useRunner && err != nil {
				t.Error("native runner shutdown failed")
			}
		case <-time.After(20 * time.Second):
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
		case err := <-done:
			t.Fatalf("generated native application exited before readiness: %v\n%s", err, runnerLogs.String())
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
