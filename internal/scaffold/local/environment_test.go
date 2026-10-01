package local

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/internal/initializer"
)

func TestEnvironmentGeneratesPrivatePerProjectAssets(t *testing.T) {
	first := generate(t, "bluebird")
	second := generate(t, "redwood")
	for _, root := range []string{first, second} {
		compose, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(compose)
		for _, want := range []string{"127.0.0.1:${AMOS_DB_PORT:-55432}:5432", PostgresImage, "bluebird-postgres-data", "bluebird-database"} {
			if root == first && !strings.Contains(text, want) {
				t.Errorf("compose.yaml missing %q", want)
			}
		}
		if root == second && (!strings.Contains(text, "redwood-postgres-data") || !strings.Contains(text, "redwood-database")) {
			t.Error("second project did not receive independent resources")
		}
		secretPath := filepath.Join(root, ".env.local")
		info, err := os.Stat(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("secret file mode=%04o, want 0600", info.Mode().Perm())
		}
		secret, err := os.ReadFile(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(secret), "AMOS_DB_MIGRATION_PASSWORD"+"=") || !strings.Contains(string(secret), "AMOS_DB_RUNTIME_PASSWORD"+"=") {
			t.Error("private file missing role credentials")
		}
		example, err := os.ReadFile(filepath.Join(root, ".env.local.example"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(example), "PASSWORD"+"=\n") == false || strings.Contains(string(example), "PASSWORD"+"=\"\"") {
			t.Error("public example must contain empty secret names only")
		}
		for _, line := range strings.Split(string(example), "\n") {
			if strings.Contains(line, "PASSWORD"+"=") && strings.TrimSpace(strings.SplitN(line, "=", 2)[1]) != "" {
				t.Error("public example contains a credential value")
			}
		}
		ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
		if err != nil || !strings.Contains(string(ignore), ".env.local") {
			t.Error("private environment file is not ignored")
		}
		for _, script := range []string{"scripts/migrate", "scripts/dev"} {
			info, err := os.Stat(filepath.Join(root, script))
			if err != nil {
				t.Errorf("missing generated %s: %v", script, err)
			} else if info.Mode().Perm() != 0755 {
				t.Errorf("generated %s mode=%04o, want 0755", script, info.Mode().Perm())
			}
			cmd := exec.Command(filepath.Join(root, script))
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 || !strings.Contains(string(output), "unavailable until") {
				t.Errorf("%s without generated app should report unavailable: err=%v output=%q", script, err, output)
			}
		}
	}
}

func TestEnvironmentPortCollision(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	if err := CheckPortAvailable(fmt.Sprint(port)); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("occupied local port result=%v, want ErrPortInUse", err)
	}
	if err := CheckPortAvailable("0"); err != nil {
		t.Fatalf("unassigned ephemeral port should be available: %v", err)
	}
}

func TestEnvironmentPodmanVolumeSurvivesContainerRestart(t *testing.T) {
	generated := generate(t, "postgres-volume-test")
	credentials := map[string]string{}
	secretData, err := os.ReadFile(filepath.Join(generated, ".env.local"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(secretData), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			credentials[key] = value
		}
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatalf("Podman prerequisite missing: %v", err)
	}
	authfile := filepath.Join(t.TempDir(), "registry-auth.json")
	if err := os.WriteFile(authfile, []byte(`{"auths":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	runPodman := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(podman, args...)
		cmd.Env = append(os.Environ(), "REGISTRY_AUTH_FILE="+authfile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Podman operation failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out))
	}
	version := runPodman("--version")
	if !strings.HasPrefix(version, "podman version ") {
		t.Fatalf("unexpected Podman version output: %s", version)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	info := exec.CommandContext(ctx, podman, "info")
	info.Env = append(os.Environ(), "REGISTRY_AUTH_FILE="+authfile)
	if out, err := info.CombinedOutput(); err != nil {
		t.Fatalf("Podman service prerequisite unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	container, volume := "amos-t74-"+suffix, "amos-t74-data-"+suffix
	t.Cleanup(func() {
		_, _ = exec.Command(podman, "rm", "-f", container).CombinedOutput()
		_, _ = exec.Command(podman, "volume", "rm", "-f", volume).CombinedOutput()
	})
	start := func(name string) {
		t.Helper()
		runPodman("run", "-d", "--name", name, "-e", "POSTGRES_USER="+credentials["AMOS_DB_MIGRATION_USER"], "-e", "POSTGRES_PASSWORD"+"="+credentials["AMOS_DB_MIGRATION_PASSWORD"], "-e", "POSTGRES_DB="+credentials["AMOS_DB_NAME"], "-e", "POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256", "-e", "POSTGRES_HOST_AUTH_METHOD=scram-sha-256", "-v", volume+":/var/lib/postgresql/data", PostgresImage)
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			cmd := exec.CommandContext(ctx, podman, "exec", name, "pg_isready", "-U", credentials["AMOS_DB_MIGRATION_USER"], "-d", credentials["AMOS_DB_NAME"])
			cmd.Env = append(os.Environ(), "REGISTRY_AUTH_FILE="+authfile)
			if cmd.Run() == nil {
				// pg_isready can report ready during the short entrypoint window
				// before initialization scripts have completed. Verify a query too.
				query := exec.CommandContext(ctx, podman, "exec", name, "psql", "-U", credentials["AMOS_DB_MIGRATION_USER"], "-d", credentials["AMOS_DB_NAME"], "-At", "-c", "SELECT 1")
				query.Env = append(os.Environ(), "REGISTRY_AUTH_FILE="+authfile)
				if out, err := query.Output(); err == nil && strings.TrimSpace(string(out)) == "1" {
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("PostgreSQL did not become ready within 45 seconds (%s)", name)
	}
	start(container)
	roleScript := filepath.Join(generated, "db-init-roles.sh")
	runPodman("cp", roleScript, container+":/tmp/roles.sh")
	runPodman("exec", "-e", "POSTGRES_USER="+credentials["AMOS_DB_MIGRATION_USER"], "-e", "POSTGRES_DB="+credentials["AMOS_DB_NAME"], "-e", "AMOS_DB_RUNTIME_USER="+credentials["AMOS_DB_RUNTIME_USER"], "-e", "AMOS_DB_RUNTIME_PASSWORD"+"="+credentials["AMOS_DB_RUNTIME_PASSWORD"], container, "sh", "/tmp/roles.sh")
	runPodman("exec", container, "psql", "-U", credentials["AMOS_DB_MIGRATION_USER"], "-d", credentials["AMOS_DB_NAME"], "-c", "CREATE TABLE volume_probe (id bigserial primary key); INSERT INTO volume_probe DEFAULT VALUES;")
	runtimeCheck := runPodman("exec", "-e", "PGPASSWORD="+credentials["AMOS_DB_RUNTIME_PASSWORD"], container, "psql", "-h", "127.0.0.1", "-q", "-U", credentials["AMOS_DB_RUNTIME_USER"], "-d", credentials["AMOS_DB_NAME"], "-At", "-c", "INSERT INTO volume_probe DEFAULT VALUES RETURNING id")
	if runtimeCheck != "2" {
		t.Fatalf("runtime role insert result=%q, want 2", runtimeCheck)
	}
	wrongPassword := exec.CommandContext(ctx, podman, "exec", "-e", "PGPASSWORD=invalid-test-password", container, "psql", "-h", "127.0.0.1", "-U", credentials["AMOS_DB_RUNTIME_USER"], "-d", credentials["AMOS_DB_NAME"], "-At", "-c", "SELECT 1")
	wrongPassword.Env = append(os.Environ(), "REGISTRY_AUTH_FILE="+authfile)
	wrongOutput, wrongErr := wrongPassword.CombinedOutput()
	if wrongErr == nil || !strings.Contains(string(wrongOutput), "password authentication failed") {
		t.Fatalf("TCP login with invalid password should be denied; err=%v output=%q", wrongErr, wrongOutput)
	}
	runPodman("rm", "-f", container)
	start(container)
	out := runPodman("exec", "-e", "PGPASSWORD="+credentials["AMOS_DB_RUNTIME_PASSWORD"], container, "psql", "-h", "127.0.0.1", "-U", credentials["AMOS_DB_RUNTIME_USER"], "-d", credentials["AMOS_DB_NAME"], "-At", "-c", "SELECT count(*) FROM volume_probe")
	if out != "2" {
		t.Fatalf("runtime role row count after restart=%q, want 2", out)
	}
}

func generate(t *testing.T, slug string) string {
	t.Helper()
	parent := t.TempDir()
	input := initializer.Input{
		SchemaVersion: initializer.SchemaVersion,
		AppSlug:       slug,
		ModulePath:    "example.test/" + slug,
		ParentDir:     parent,
		Target:        slug,
		Modules:       []string{"workspace", "identity"},
		PublicOrigin:  "http://127.0.0.1:8080",
		BusinessMode:  "integrated-go",
		Mode:          "evaluation",
	}
	result, err := initializer.Initialize(context.Background(), input, Generator{})
	if err != nil {
		t.Fatalf("initialize %s: %v", slug, err)
	}
	return filepath.Join(parent, result.Target)
}
