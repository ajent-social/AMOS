package devrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeLifecycleHelper(t *testing.T) {
	if os.Getenv("AMOS_DEVRUNNER_TEST_CHILD") != "1" {
		return
	}
	if len(os.Args) < 2 {
		os.Exit(30)
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "orphan" {
		signal.Ignore(os.Interrupt, syscall.SIGTERM)
		_ = os.WriteFile(os.Getenv("AMOS_DEVRUNNER_ORPHAN_PIDFILE"), []byte(strconv.Itoa(os.Getpid())), 0600)
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "server" && os.Getenv("AMOS_DEVRUNNER_EARLY_ORPHAN") == "1" {
		child := exec.Command(os.Args[0], "-test.run=^TestNativeLifecycleHelper$", "--", "orphan")
		child.Env = os.Environ()
		if child.Start() != nil {
			os.Exit(34)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, e := os.Stat(os.Getenv("AMOS_DEVRUNNER_ORPHAN_PIDFILE")); e == nil {
				os.Exit(18)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(35)
	}

	marker := os.Getenv("AMOS_DEVRUNNER_MARKER")
	if marker != "" {
		content := fmt.Sprintf("%s migration=%t runtime=%t\n", mode, os.Getenv("AMOS_MIGRATION_DATABASE_URL") != "", os.Getenv("AMOS_DATABASE_URL") != "")
		f, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(31)
		}
		_, _ = io.WriteString(f, content)
		_ = f.Close()
	}
	if mode == "migration" {
		_, _ = fmt.Fprintln(os.Stdout, "migration connection", os.Getenv("AMOS_MIGRATION_DATABASE_URL"))
		if os.Getenv("AMOS_DEVRUNNER_FAIL_MIGRATION") == "1" {
			os.Exit(17)
		}
		return
	}
	if mode != "server" {
		os.Exit(32)
	}
	pidPath := os.Getenv("AMOS_DEVRUNNER_PIDFILE")
	if pidPath != "" {
		_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600)
	}
	_, _ = fmt.Fprintln(os.Stdout, "runtime connection", os.Getenv("AMOS_DATABASE_URL"))
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if os.Getenv("AMOS_DEVRUNNER_NEVER_READY") == "1" {
		<-signals
		return
	}
	address := os.Getenv("AMOS_DEVRUNNER_ADDRESS")
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		os.Exit(33)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ready")
			return
		}
		http.NotFound(w, r)
	})}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	if os.Getenv("AMOS_DEVRUNNER_EXIT_SERVER") == "1" {
		select {
		case <-time.After(500 * time.Millisecond):
			os.Exit(18)
		case <-signals:
		}
	} else {
		<-signals
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	_ = listener.Close()
	<-serverDone
}

func TestLifecycleGracefulStopKeepsVolumeAndReleasesPort(t *testing.T) {
	project := newTestProject(t)
	readiness := freeURL(t)
	marker := filepath.Join(t.TempDir(), "calls")
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	var logs bytes.Buffer
	options := testOptions(project, readiness, &logs)
	options.AppReadyTimeout = 20 * time.Second
	t.Setenv("AMOS_DEVRUNNER_TEST_CHILD", "1")
	t.Setenv("AMOS_DEVRUNNER_MARKER", marker)
	t.Setenv("AMOS_DEVRUNNER_PIDFILE", pidFile)
	t.Setenv("AMOS_DEVRUNNER_ADDRESS", readinessAddress(readiness))
	t.Setenv("AMOS_DEVRUNNER_NEVER_READY", "")
	t.Setenv("AMOS_DEVRUNNER_EXIT_SERVER", "")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Run(ctx, options) }()
	waitRunReady(t, readiness, result)
	select {
	case runErr := <-result:
		t.Fatalf("runner stopped immediately after readiness: %v", runErr)
	default:
	}
	databaseID := waitProjectDatabase(t, options.Project)
	select {
	case runErr := <-result:
		t.Fatalf("runner stopped before record creation: %v", runErr)
	default:
	}
	status, err := InspectProject(context.Background(), StatusOptions{Project: options.Project, WorkingDir: options.WorkingDir, PodmanBinary: options.PodmanBinary})
	if err != nil || status.State != ProjectRunning || status.Process.State != ResourceRunning {
		t.Fatalf("owned running status unavailable: %v %+v", err, status)
	}
	for _, resource := range status.Resources {
		if resource.State == ResourceUnknown {
			t.Fatal("owned resource status unknown")
		}
	}
	createRecord(t, databaseID, project.config)
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("graceful cancellation: %v", err)
	}
	store, err := newPrivateStateStore(options.WorkingDir, options.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil || state.Process != nil {
		t.Fatal("owned process record retained after observed shutdown")
	}
	if got := strings.Count(readFile(t, marker), "migration "); got != 1 {
		t.Fatalf("migration invocation count=%d, want 1", got)
	}
	if content := readFile(t, marker); !strings.Contains(content, "migration migration=true runtime=false") || !strings.Contains(content, "server migration=false runtime=true") {
		t.Fatalf("subprocess environment roles were not isolated: %q", content)
	}
	pid := readPID(t, pidFile)
	assertProcessGone(t, pid)
	assertPortReleased(t, readiness)
	assertVolumeRecord(t, options.Project, project.config)
	if strings.Contains(logs.String(), project.config.migrationPassword) || strings.Contains(logs.String(), project.config.runtimePassword) || strings.Contains(logs.String(), "postgres://amos_") {
		t.Fatalf("subprocess output leaked a database URL or password")
	}
	if !strings.Contains(logs.String(), "postgres://<redacted>") {
		t.Fatalf("database URL was not replaced by a redaction marker: %q", logs.String())
	}
	if !strings.Contains(logs.String(), "[migration]") || !strings.Contains(logs.String(), "[app]") {
		t.Fatalf("subprocess output is missing labels: %q", logs.String())
	}
}

func TestLifecycleStartupTimeoutKillsChildAndPreservesUnrelatedContainer(t *testing.T) {
	project := newTestProject(t)
	readiness := freeURL(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	unrelated := "amos-t75-unrelated-" + randomSuffix(t)
	podman := podmanPath(t)
	podmanRun(t, podman, "run", "-d", "--name", unrelated, postgresImage, "sleep", "300")
	t.Cleanup(func() { _, _ = exec.Command(podman, "rm", "-f", unrelated).CombinedOutput() })
	options := testOptions(project, readiness, io.Discard)
	options.AppReadyTimeout = 800 * time.Millisecond
	options.ShutdownGrace = time.Second
	t.Setenv("AMOS_DEVRUNNER_TEST_CHILD", "1")
	t.Setenv("AMOS_DEVRUNNER_MARKER", filepath.Join(t.TempDir(), "calls"))
	t.Setenv("AMOS_DEVRUNNER_PIDFILE", pidFile)
	t.Setenv("AMOS_DEVRUNNER_ADDRESS", readinessAddress(readiness))
	t.Setenv("AMOS_DEVRUNNER_NEVER_READY", "1")
	err := Run(context.Background(), options)
	if !errors.Is(err, ErrAppTimeout) {
		t.Fatalf("startup timeout error=%v, want ErrAppTimeout", err)
	}
	assertProcessGone(t, readPID(t, pidFile))
	status := podmanRun(t, podman, "inspect", "--format", "{{.State.Running}}", unrelated)
	if status != "true" {
		t.Fatalf("unrelated container status=%q, want true", status)
	}
}

func TestLifecycleMigrationAndServerFailuresReturnErrors(t *testing.T) {
	for _, scenario := range []string{"migration", "server"} {
		t.Run(scenario, func(t *testing.T) {
			project := newTestProject(t)
			readiness := freeURL(t)
			options := testOptions(project, readiness, io.Discard)
			options.AppReadyTimeout = 10 * time.Second
			t.Setenv("AMOS_DEVRUNNER_TEST_CHILD", "1")
			t.Setenv("AMOS_DEVRUNNER_MARKER", filepath.Join(t.TempDir(), "calls"))
			t.Setenv("AMOS_DEVRUNNER_PIDFILE", filepath.Join(t.TempDir(), "child.pid"))
			t.Setenv("AMOS_DEVRUNNER_ADDRESS", readinessAddress(readiness))
			if scenario == "migration" {
				t.Setenv("AMOS_DEVRUNNER_FAIL_MIGRATION", "1")
				t.Setenv("AMOS_DEVRUNNER_NEVER_READY", "")
				t.Setenv("AMOS_DEVRUNNER_EXIT_SERVER", "")
			} else {
				t.Setenv("AMOS_DEVRUNNER_FAIL_MIGRATION", "")
				t.Setenv("AMOS_DEVRUNNER_NEVER_READY", "")
				t.Setenv("AMOS_DEVRUNNER_EXIT_SERVER", "1")
			}
			if err := Run(context.Background(), options); err == nil {
				t.Fatal("runner reported success after subprocess failure")
			}
		})
	}
}

func TestLifecycleValidationRejectsDSNArgumentsAndNonLoopbackReadyURL(t *testing.T) {
	for _, unsafeArg := range []string{"postgres://user:pass@localhost/db", "POSTGRESQL://user:pass@localhost/db", "PASSWORD" + "=test"} {
		base := Options{Project: "test-app", WorkingDir: t.TempDir(), AppBinary: os.Args[0], ReadinessURL: "http://127.0.0.1:1/readyz", MigrationArgs: []string{"migrate", unsafeArg}}
		if err := validateOptions(&base); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("unsafe argument %q error=%v", unsafeArg, err)
		}
	}
	if err := checkReadinessURL("http://example.test/readyz"); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("external readiness URL error=%v", err)
	}
}

type testProject struct {
	dir    string
	slug   string
	config localConfig
}

func newTestProject(t *testing.T) testProject {
	t.Helper()
	requirePodman(t)
	slug := "amos-runner-" + randomSuffix(t)
	path, err := os.MkdirTemp(os.TempDir(), slug+"-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	config := localConfig{port: freePort(t), database: "amos_dev", migrationUser: "amos_migrator", migrationPassword: randomSuffix(t) + randomSuffix(t), runtimeUser: "amos_runtime", runtimePassword: randomSuffix(t) + randomSuffix(t)}
	privateEnv := fmt.Sprintf("AMOS_DB_PORT=%s\nAMOS_DB_NAME=%s\nAMOS_DB_MIGRATION_USER=%s\nAMOS_DB_MIGRATION_"+"PASSWORD"+"=%s\nAMOS_DB_RUNTIME_USER=%s\nAMOS_DB_RUNTIME_"+"PASSWORD"+"=%s\n", config.port, config.database, config.migrationUser, config.migrationPassword, config.runtimeUser, config.runtimePassword)
	if err := os.WriteFile(filepath.Join(path, ".env.local"), []byte(privateEnv), 0600); err != nil {
		t.Fatal(err)
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source to load generated role initializer")
	}
	roleScript, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "..", "scaffold", "local", "templates", "db-init-roles.sh.tmpl"))
	if err != nil {
		t.Fatalf("read generated PostgreSQL role initializer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "db-init-roles.sh"), roleScript, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		podman := podmanPath(t)
		containers, _ := exec.Command(podman, "ps", "-a", "--filter", "label=io.amos.project="+slug, "--format", "{{.ID}}").Output()
		for _, id := range strings.Fields(string(containers)) {
			_, _ = exec.Command(podman, "rm", "-f", id).CombinedOutput()
		}
		_, _ = exec.Command(podman, "volume", "rm", "-f", slug+"-postgres-data").CombinedOutput()
		_, _ = exec.Command(podman, "network", "rm", slug+"-database").CombinedOutput()
	})
	return testProject{dir: path, slug: slug, config: config}
}

func testOptions(project testProject, readiness string, output io.Writer) Options {
	return Options{Project: project.slug, WorkingDir: project.dir, AppBinary: os.Args[0], ReadinessURL: readiness, MigrationArgs: []string{"-test.run=^TestNativeLifecycleHelper$", "--", "migration"}, ServerArgs: []string{"-test.run=^TestNativeLifecycleHelper$", "--", "server"}, DatabaseReadyTimeout: 45 * time.Second, AppReadyTimeout: 15 * time.Second, ShutdownGrace: 2 * time.Second, Output: output}
}

func requirePodman(t *testing.T) {
	t.Helper()
	podman := podmanPath(t)
	authFile := filepath.Join(t.TempDir(), "registry-auth.json")
	if err := os.WriteFile(authFile, []byte(`{"auths":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REGISTRY_AUTH_FILE", authFile)
	if out, err := exec.Command(podman, "info").CombinedOutput(); err != nil {
		t.Fatalf("Podman prerequisite unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(podman, "image", "exists", postgresImage).CombinedOutput(); err != nil {
		t.Fatalf("pinned PostgreSQL image prerequisite unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func podmanPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("podman")
	if err != nil {
		t.Fatalf("Podman prerequisite missing: %v", err)
	}
	return path
}

func podmanRun(t *testing.T, podman string, args ...string) string {
	t.Helper()
	cmd := exec.Command(podman, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Podman test operation failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return strconv.Itoa(port)
}

func freeURL(t *testing.T) string {
	return "http://127.0.0.1:" + freePort(t) + "/readyz"
}

func readinessAddress(endpoint string) string {
	u, _ := url.Parse(endpoint)
	return u.Host
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	var data [5]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data[:])
}

func waitRunReady(t *testing.T, endpoint string, result <-chan error) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		request, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case runErr := <-result:
			t.Fatalf("runner exited before readiness: %v", runErr)
		case <-deadline.C:
			t.Fatal("app readiness did not become available")
		case <-ticker.C:
		}
	}
}

func waitProjectDatabase(t *testing.T, project string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command(podmanPath(t), "ps", "--filter", "label=io.amos.project="+project, "--filter", "label=io.amos.resource=database", "--format", "{{.ID}}").Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return strings.TrimSpace(string(output))
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("owned PostgreSQL container did not remain running")
	return ""
}

func createRecord(t *testing.T, container string, config localConfig) {
	t.Helper()
	cmd := exec.Command(podmanPath(t), "exec", container, "psql", "-U", config.migrationUser, "-d", config.database, "-c", "CREATE TABLE runner_probe (id bigserial primary key); INSERT INTO runner_probe DEFAULT VALUES;")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("migration-owned verification record could not be written: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

func assertVolumeRecord(t *testing.T, project string, config localConfig) {
	t.Helper()
	podman := podmanPath(t)
	volume := project + "-postgres-data"
	verifyName := project + "-verify"
	image := postgresImage
	cmd := exec.Command(podman, "run", "-d", "--name", verifyName, "-e", "POSTGRES_USER", "-e", "POSTGRES_PASSWORD", "-e", "POSTGRES_DB", "-v", volume+":/var/lib/postgresql/data", image)
	cmd.Env = mergeEnvironment(os.Environ(), databaseEnvironment(config))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start volume verification container: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { _, _ = exec.Command(podman, "rm", "-f", verifyName).CombinedOutput() })
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command(podman, "exec", verifyName, "pg_isready", "-U", config.migrationUser, "-d", config.database)
		if cmd.Run() == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd = exec.Command(podman, "exec", "--env", "PGPASSWORD", verifyName, "psql", "-h", "127.0.0.1", "-U", config.runtimeUser, "-d", config.database, "-At", "-c", "SELECT count(*) FROM runner_probe")
	cmd.Env = mergeEnvironment(os.Environ(), map[string]string{"PGPASSWORD": config.runtimePassword})
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "1" {
		t.Fatalf("stored record after runner stop count=%q err=%v", strings.TrimSpace(string(output)), err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	text := strings.TrimSpace(readFile(t, path))
	pid, err := strconv.Atoi(text)
	if err != nil || pid < 1 {
		t.Fatalf("invalid child pid marker: %q", text)
	}
	return pid
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	err := syscall.Kill(pid, 0)
	if err == nil || !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child process %d remains alive after stop: %v", pid, err)
	}
}

func assertPortReleased(t *testing.T, endpoint string) {
	t.Helper()
	u, _ := url.Parse(endpoint)
	listener, err := net.Listen("tcp4", u.Host)
	if err != nil {
		t.Fatalf("app port remains occupied after graceful stop: %v", err)
	}
	_ = listener.Close()
}

func TestEarlyExitReadinessRetainsCompletedChild(t *testing.T) {
	done := &processCompletion{done: make(chan struct{}), result: errors.New("synthetic child exit")}
	close(done.done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if err := waitForReady(ctx, "http://127.0.0.1:1/readyz", done); err == nil {
			t.Fatal("early exit lost broadcast completion")
		}
	}
}

func TestEarlyNativeParentExitStopsSurvivingOwnedGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("AMOS_DEVRUNNER_TEST_CHILD", "1")
	t.Setenv("AMOS_DEVRUNNER_EARLY_ORPHAN", "1")
	t.Setenv("AMOS_DEVRUNNER_ORPHAN_PIDFILE", pidFile)
	t.Cleanup(func() {
		if b, e := os.ReadFile(pidFile); e == nil {
			if pid, e := strconv.Atoi(string(b)); e == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	cfg := localConfig{port: "1", database: "fixture", runtimeUser: "fixture", runtimePassword: "fixture"}
	workingDir := t.TempDir()
	identity, err := InitializeProject(context.Background(), workingDir, "early-exit")
	if err != nil {
		t.Fatal(err)
	}
	runner := &resources{identity: identity, project: identity.Project, config: cfg, redactor: newRedactor(cfg), options: Options{AppBinary: os.Args[0], WorkingDir: workingDir, Project: identity.Project, ServerArgs: []string{"-test.run=^TestNativeLifecycleHelper$", "--", "server"}, ReadinessURL: freeURL(t), AppReadyTimeout: 2 * time.Second, ShutdownGrace: 100 * time.Millisecond, Output: io.Discard}}
	result := make(chan error, 1)
	go func() { result <- runner.runServer(context.Background()) }()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("early child exit succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup cleanup hung after parent exit")
	}
	assertProcessGone(t, readPID(t, pidFile))
}

func TestRunnerResourceIdentityLabels(t *testing.T) {
	identity, err := InitializeProject(context.Background(), t.TempDir(), "label-boundary")
	if err != nil {
		t.Fatal(err)
	}
	r := resources{identity: identity, project: identity.Project}
	labels, err := ResourceLabels(identity, ResourceDatabaseVolume)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ownsLabels(labels, ResourceDatabaseVolume) {
		t.Fatal("owned identity labels denied")
	}
	labels[projectIDLabel] = "018f15e2-9aba-7d99-9fd0-070ca95bb1f8"
	if r.ownsLabels(labels, ResourceDatabaseVolume) {
		t.Fatal("foreign UUID resource adopted")
	}
	labels[projectIDLabel] = identity.ID.String()
	labels[resourceLabel] = string(ResourceDatabaseNetwork)
	if r.ownsLabels(labels, ResourceDatabaseVolume) {
		t.Fatal("wrong-kind resource adopted")
	}
	if r.ownsLabels(map[string]string{"com.docker.compose.project": identity.Project}, ResourceDatabaseVolume) {
		t.Fatal("legacy compose labels adopted")
	}
}
func TestLifecycleRejectsSameSlugForeignVolume(t *testing.T) {
	requirePodman(t)
	project := newTestProject(t)
	options := testOptions(project, freeURL(t), io.Discard)
	volume := options.Project + "-postgres-data"
	podmanRun(t, podmanPath(t), "volume", "create", "--label", projectLabel+"="+options.Project, "--label", projectIDLabel+"=018f15e2-9aba-7d99-9fd0-070ca95bb1f8", "--label", resourceLabel+"="+string(ResourceDatabaseVolume), volume)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Run(ctx, options); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("foreign same-slug volume result = %v, want ownership conflict", err)
	}
	podmanRun(t, podmanPath(t), "volume", "exists", volume)
}

func TestRunnerRuntimeDatabaseAlias(t *testing.T) {
	r := resources{config: localConfig{port: "5432", database: "fixture", runtimeUser: "fixture", runtimePassword: "fixture"}}
	env := r.serverEnvironment()
	if env["AMOS_RUNTIME_DATABASE_URL"] == "" || env["AMOS_RUNTIME_DATABASE_URL"] != env["AMOS_DATABASE_URL"] {
		t.Fatal("native runtime database alias unavailable")
	}
	const runtimeURLKey = "AMOS_RUNTIME_DATABASE_URL"
	for _, entry := range mergeEnvironment([]string{runtimeURLKey + "=test"}, r.migrationEnvironment()) {
		if strings.HasPrefix(entry, runtimeURLKey+"=") {
			t.Fatal("runtime database credential inherited by migration child")
		}
	}
}
