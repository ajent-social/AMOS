// Package devrunner supervises the generated application's local development
// process and only the PostgreSQL resources assigned to its project.
package devrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultDatabaseReadyTimeout = 45 * time.Second
	defaultAppReadyTimeout      = 30 * time.Second
	defaultShutdownGrace        = 5 * time.Second
	commandTimeout              = 15 * time.Second
	maximumOutput               = 1 << 20
	postgresImage               = "docker.io/library/postgres:16.15-bookworm@sha256:efedf3595f1d6f415c08568ba171029bf54052e754cc9f030e3f2412b21f3d67"
)

var (
	ErrInvalidOptions   = errors.New("invalid local development runner options")
	ErrResourceConflict = errors.New("local database resource is not owned by this project")
	ErrDatabaseTimeout  = errors.New("local database did not become ready before timeout")
	ErrAppTimeout       = errors.New("local application did not become ready before timeout")
)

// Options identify one generated app. AppBinary must be a native executable
// built by the caller; the runner never supervises a compiler wrapper such as
// `go run`, which could leave the generated server orphaned on cancellation.
type Options struct {
	Project              string
	WorkingDir           string
	AppBinary            string
	ReadinessURL         string
	PodmanBinary         string
	MigrationArgs        []string
	ServerArgs           []string
	DatabaseReadyTimeout time.Duration
	AppReadyTimeout      time.Duration
	ShutdownGrace        time.Duration
	Output               io.Writer
}

type localConfig struct {
	port, database, migrationUser, migrationPassword, runtimeUser, runtimePassword string
}

type resources struct {
	project, volume, network, container string
	createdNetwork                      bool
	containerID                         string
	config                              localConfig
	options                             Options
	redactor                            *redactor
	extraEnv                            map[string]string
	outputMu                            sync.Mutex
}

// Run starts this project's local PostgreSQL container, applies migrations
// once, starts the native app child, and waits for cancellation or child exit.
// Cancellation performs a bounded graceful stop and preserves the data volume.
func Run(ctx context.Context, options Options) (runErr error) {
	if ctx == nil {
		return ErrInvalidOptions
	}
	if err := validateOptions(&options); err != nil {
		return err
	}
	config, err := readLocalConfig(filepath.Join(options.WorkingDir, ".env.local"))
	if err != nil {
		return err
	}
	if err := checkAppBinary(options.AppBinary); err != nil {
		return err
	}
	if err := checkReadinessURL(options.ReadinessURL); err != nil {
		return err
	}
	if err := checkPort(config.port); err != nil {
		return err
	}
	r := &resources{
		project:  options.Project,
		volume:   options.Project + "-postgres-data",
		network:  options.Project + "-database",
		options:  options,
		config:   config,
		redactor: newRedactor(config),
	}
	defer func() {
		if cleanupErr := r.cleanup(); cleanupErr != nil {
			runErr = errors.Join(runErr, cleanupErr)
		}
	}()

	if err := r.ensureNetwork(ctx); err != nil {
		return err
	}
	if err := r.ensureVolume(ctx); err != nil {
		return err
	}
	if err := r.startDatabase(ctx); err != nil {
		return err
	}
	if err := r.waitDatabase(ctx); err != nil {
		return err
	}
	if err := r.runAppCommand(ctx, options.MigrationArgs, "migration", r.migrationEnvironment()); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	return r.runServer(ctx)
}

func validateOptions(options *Options) error {
	if options.Project == "" || len(options.Project) > 63 || !slugRE.MatchString(options.Project) || options.WorkingDir == "" || options.AppBinary == "" {
		return ErrInvalidOptions
	}
	workingDir, err := filepath.Abs(options.WorkingDir)
	if err != nil {
		return ErrInvalidOptions
	}
	options.WorkingDir = workingDir
	if !filepath.IsAbs(options.AppBinary) {
		options.AppBinary = filepath.Join(workingDir, options.AppBinary)
	}
	if options.PodmanBinary == "" {
		options.PodmanBinary = "podman"
	}
	if len(options.MigrationArgs) == 0 {
		options.MigrationArgs = []string{"migrate"}
	}
	if len(options.ServerArgs) == 0 {
		options.ServerArgs = []string{"serve"}
	}
	if options.ReadinessURL == "" {
		options.ReadinessURL = "http://127.0.0.1:8080/readyz"
	}
	if options.DatabaseReadyTimeout == 0 {
		options.DatabaseReadyTimeout = defaultDatabaseReadyTimeout
	}
	if options.AppReadyTimeout == 0 {
		options.AppReadyTimeout = defaultAppReadyTimeout
	}
	if options.ShutdownGrace == 0 {
		options.ShutdownGrace = defaultShutdownGrace
	}
	if options.DatabaseReadyTimeout < 100*time.Millisecond || options.DatabaseReadyTimeout > 5*time.Minute || options.AppReadyTimeout < 100*time.Millisecond || options.AppReadyTimeout > 5*time.Minute || options.ShutdownGrace < 100*time.Millisecond || options.ShutdownGrace > 30*time.Second {
		return ErrInvalidOptions
	}
	for _, arg := range append(append([]string{}, options.MigrationArgs...), options.ServerArgs...) {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "postgres://") || strings.Contains(lower, "postgresql://") || strings.Contains(lower, "password=") {
			return ErrInvalidOptions
		}
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	return nil
}

var slugRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func readLocalConfig(path string) (localConfig, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 {
		return localConfig{}, errors.New("local private database configuration is unavailable or has unsafe permissions")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 16<<10 {
		return localConfig{}, errors.New("local private database configuration could not be read")
	}
	values := make(map[string]string, 6)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return localConfig{}, errors.New("local private database configuration is malformed")
		}
		if _, exists := values[key]; exists {
			return localConfig{}, errors.New("local private database configuration contains duplicate names")
		}
		values[key] = value
	}
	allowed := map[string]bool{
		"AMOS_DB_PORT": true, "AMOS_DB_NAME": true,
		"AMOS_DB_MIGRATION_USER": true, "AMOS_DB_MIGRATION_PASSWORD": true,
		"AMOS_DB_RUNTIME_USER": true, "AMOS_DB_RUNTIME_PASSWORD": true,
	}
	for key := range values {
		if !allowed[key] {
			return localConfig{}, errors.New("local private database configuration has an unsupported name")
		}
	}
	for key := range allowed {
		if values[key] == "" {
			return localConfig{}, errors.New("local private database configuration is incomplete")
		}
	}
	port, err := strconv.Atoi(values["AMOS_DB_PORT"])
	if err != nil || port < 1 || port > 65535 || !sqlIdentifierRE.MatchString(values["AMOS_DB_NAME"]) || !sqlIdentifierRE.MatchString(values["AMOS_DB_MIGRATION_USER"]) || !sqlIdentifierRE.MatchString(values["AMOS_DB_RUNTIME_USER"]) {
		return localConfig{}, errors.New("local private database configuration has invalid names")
	}
	return localConfig{port: strconv.Itoa(port), database: values["AMOS_DB_NAME"], migrationUser: values["AMOS_DB_MIGRATION_USER"], migrationPassword: values["AMOS_DB_MIGRATION_PASSWORD"], runtimeUser: values["AMOS_DB_RUNTIME_USER"], runtimePassword: values["AMOS_DB_RUNTIME_PASSWORD"]}, nil
}

var sqlIdentifierRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func checkAppBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("native app executable is unavailable")
	}
	return nil
}

func checkReadinessURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" || (u.Hostname() != "localhost" && !isLoopback(u.Hostname())) || u.User != nil || u.Fragment != "" {
		return ErrInvalidOptions
	}
	if u.Path != "/readyz" {
		return ErrInvalidOptions
	}
	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func checkPort(port string) error {
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", port), 250*time.Millisecond)
	if err != nil {
		return nil
	}
	_ = conn.Close()
	return errors.New("local database port is already occupied")
}

func (r *resources) ensureNetwork(ctx context.Context) error {
	return r.ensureNamedResource(ctx, "network", r.network, &r.createdNetwork, []string{"network", "exists", r.network}, []string{"network", "inspect", "--format", "{{json .Labels}}", r.network}, []string{"network", "create", "--label", "io.amos.project=" + r.project, r.network})
}

func (r *resources) ensureVolume(ctx context.Context) error {
	var created bool
	err := r.ensureNamedResource(ctx, "volume", r.volume, &created, []string{"volume", "exists", r.volume}, []string{"volume", "inspect", "--format", "{{json .Labels}}", r.volume}, []string{"volume", "create", "--label", "io.amos.project=" + r.project, r.volume})
	return err
}

func (r *resources) ensureNamedResource(ctx context.Context, kind, name string, created *bool, existsArgs, inspectArgs, createArgs []string) error {
	_, err := r.podman(ctx, "", existsArgs...)
	if err == nil {
		out, inspectErr := r.podman(ctx, "", inspectArgs...)
		if inspectErr != nil {
			return fmt.Errorf("inspect existing local %s: %w", kind, inspectErr)
		}
		var labels map[string]string
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &labels) != nil || (labels["io.amos.project"] != r.project && labels["com.docker.compose.project"] != r.project) {
			return ErrResourceConflict
		}
		return nil
	}
	var commandErr *commandError
	if !errors.As(err, &commandErr) || commandErr.code != 1 {
		return fmt.Errorf("check local %s resource: %w", kind, err)
	}
	if _, err := r.podman(ctx, "", createArgs...); err != nil {
		return fmt.Errorf("create local %s resource: %w", kind, err)
	}
	*created = kind == "network"
	_ = name
	return nil
}

func (r *resources) startDatabase(ctx context.Context) error {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return errors.New("could not allocate local database container identity")
	}
	r.container = r.project + "-db-" + hex.EncodeToString(suffix[:])
	roleScript, err := filepath.Abs(filepath.Join(r.options.WorkingDir, "db-init-roles.sh"))
	if err != nil {
		return errors.New("local database role initializer path is invalid")
	}
	info, err := os.Lstat(roleScript)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("local database role initializer is unavailable")
	}
	env := databaseEnvironment(r.config)
	args := []string{"create", "--name", r.container,
		"--label", "io.amos.project=" + r.project,
		"--label", "io.amos.resource=database",
		"--network", r.network,
		"--publish", "127.0.0.1:" + r.config.port + ":5432",
		"--volume", r.volume + ":/var/lib/postgresql/data",
		"--env", "POSTGRES_DB", "--env", "POSTGRES_USER", "--env", "POSTGRES_PASSWORD",
		"--env", "POSTGRES_INITDB_ARGS", "--env", "POSTGRES_HOST_AUTH_METHOD",
		"--env", "AMOS_DB_RUNTIME_USER", "--env", "AMOS_DB_RUNTIME_PASSWORD",
		postgresImage}
	oldEnv := r.extraEnv
	r.extraEnv = env
	out, err := r.podman(ctx, "database", args...)
	r.extraEnv = oldEnv
	if err != nil {
		return fmt.Errorf("create local database container: %w", err)
	}
	r.containerID = strings.TrimSpace(out)
	if r.containerID == "" || strings.ContainsAny(r.containerID, "\r\n") {
		return errors.New("Podman did not return the created database identity")
	}
	if _, err := r.podman(ctx, "", "cp", roleScript, r.containerID+":/docker-entrypoint-initdb.d/10-amos-roles.sh"); err != nil {
		return fmt.Errorf("copy generated local database role initializer: %w", err)
	}
	if _, err := r.podman(ctx, "database", "start", r.containerID); err != nil {
		return fmt.Errorf("start local database container: %w", err)
	}
	return nil
}

func (r *resources) waitDatabase(ctx context.Context) error {
	deadline := time.NewTimer(r.options.DatabaseReadyTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		out, err := r.podman(ctx, "database", "exec", r.containerID, "pg_isready", "-U", r.config.migrationUser, "-d", r.config.database)
		conn, dialErr := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", r.config.port), 250*time.Millisecond)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil && strings.Contains(out, "accepting connections") && dialErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return ErrDatabaseTimeout
		case <-ticker.C:
		}
	}
}

func (r *resources) runAppCommand(ctx context.Context, args []string, label string, env map[string]string) error {
	cmd := exec.Command(r.options.AppBinary, args...)
	cmd.Dir = r.options.WorkingDir
	cmd.Env = mergeEnvironment(os.Environ(), env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stream := &labelWriter{label: label, out: r.options.Output, redactor: r.redactor, mu: &r.outputMu}
	cmd.Stdout, cmd.Stderr = stream, stream
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s process: %w", label, err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		stream.Flush()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stopErr := stopProcess(cmd, done, r.options.ShutdownGrace)
		return errors.Join(ctx.Err(), stopErr)
	}
}

func (r *resources) runServer(ctx context.Context) error {
	cmd := exec.Command(r.options.AppBinary, r.options.ServerArgs...)
	cmd.Dir = r.options.WorkingDir
	cmd.Env = mergeEnvironment(os.Environ(), r.serverEnvironment())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stream := &labelWriter{label: "app", out: r.options.Output, redactor: r.redactor, mu: &r.outputMu}
	cmd.Stdout, cmd.Stderr = stream, stream
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start local application: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		stream.Flush()
		done <- err
	}()

	readyCtx, cancelReady := context.WithTimeout(ctx, r.options.AppReadyTimeout)
	readyErr := waitForReady(readyCtx, r.options.ReadinessURL, done)
	cancelReady()
	if readyErr != nil {
		stopErr := stopProcess(cmd, done, r.options.ShutdownGrace)
		if errors.Is(readyErr, context.Canceled) && ctx.Err() != nil {
			if stopErr != nil && !errors.Is(stopErr, os.ErrProcessDone) {
				return stopErr
			}
			return nil
		}
		if errors.Is(readyErr, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %v", ErrAppTimeout, stopErr)
		}
		return readyErr
	}
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("local application exited: %w", err)
		}
		return errors.New("local application exited before cancellation")
	case <-ctx.Done():
		if err := stopProcess(cmd, done, r.options.ShutdownGrace); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("stop local application: %w", err)
		}
		return nil
	}
}

func waitForReady(ctx context.Context, endpoint string, done <-chan error) error {
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		response, err := client.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("local application exited during startup: %w", err)
			}
			return errors.New("local application exited during startup")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func stopProcess(cmd *exec.Cmd, done <-chan error, grace time.Duration) error {
	if cmd.Process == nil {
		return nil
	}
	var failures []error
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
		failures = append(failures, fmt.Errorf("interrupt local process group: %w", err))
		if signalErr := cmd.Process.Signal(os.Interrupt); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
			failures = append(failures, fmt.Errorf("interrupt local process: %w", signalErr))
		}
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			failures = append(failures, fmt.Errorf("kill local process group: %w", err))
		}
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			failures = append(failures, fmt.Errorf("kill local process: %w", err))
		}
		<-done
		return errors.Join(failures...)
	}
}

func (r *resources) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	var failures []error
	if r.containerID != "" {
		state, inspectErr := r.podman(ctx, "", "inspect", "--format", "{{.State.Running}}", r.containerID)
		if inspectErr == nil && state == "true" {
			if _, err := r.podman(ctx, "database", "stop", "--time", "5", r.containerID); err != nil {
				failures = append(failures, fmt.Errorf("stop owned local database: %w", err))
			}
		} else if inspectErr != nil {
			failures = append(failures, fmt.Errorf("inspect owned local database before cleanup: %w", inspectErr))
		}
		if _, err := r.podman(ctx, "database", "rm", r.containerID); err != nil {
			failures = append(failures, fmt.Errorf("remove owned local database: %w", err))
		}
	}
	if r.createdNetwork {
		if _, err := r.podman(ctx, "database", "network", "rm", r.network); err != nil {
			failures = append(failures, fmt.Errorf("remove owned local database network: %w", err))
		}
	}
	return errors.Join(failures...)
}

func (r *resources) podman(ctx context.Context, label string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, r.options.PodmanBinary, args...)
	cmd.Env = mergeEnvironment(os.Environ(), r.extraEnv)
	var stdout cappedBuffer
	var stderr cappedBuffer
	stdout.limit, stderr.limit = maximumOutput, maximumOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return "", errors.New("Podman response exceeded the output limit")
	}
	if label != "" && stdout.Len() != 0 {
		r.outputMu.Lock()
		_, _ = io.WriteString(r.options.Output, r.redactor.line("["+label+"] "+strings.TrimSpace(stdout.String())+"\n"))
		r.outputMu.Unlock()
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			message = r.redactor.clean(message)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", &commandError{code: exitErr.ExitCode(), output: message}
		}
		return "", fmt.Errorf("Podman operation failed: %s", message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *resources) migrationEnvironment() map[string]string {
	return map[string]string{"AMOS_MIGRATION_DATABASE_URL": r.databaseURL(r.config.migrationUser, r.config.migrationPassword)}
}

func (r *resources) serverEnvironment() map[string]string {
	return map[string]string{"AMOS_DATABASE_URL": r.databaseURL(r.config.runtimeUser, r.config.runtimePassword)}
}

func (r *resources) databaseURL(user, password string) string {
	return "postgres://" + user + ":" + password + "@127.0.0.1:" + r.config.port + "/" + r.config.database + "?sslmode=disable"
}

func databaseEnvironment(config localConfig) map[string]string {
	return map[string]string{
		"POSTGRES_DB":               config.database,
		"POSTGRES_USER":             config.migrationUser,
		"POSTGRES_PASSWORD":         config.migrationPassword,
		"POSTGRES_INITDB_ARGS":      "--auth-host=scram-sha-256",
		"POSTGRES_HOST_AUTH_METHOD": "scram-sha-256",
		"AMOS_DB_RUNTIME_USER":      config.runtimeUser,
		"AMOS_DB_RUNTIME_PASSWORD":  config.runtimePassword,
	}
}

func mergeEnvironment(base []string, additions map[string]string) []string {
	values := make(map[string]string, len(base)+len(additions))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok && !strings.HasPrefix(key, "AMOS_DB_") && key != "AMOS_DATABASE_URL" && key != "AMOS_MIGRATION_DATABASE_URL" && key != "PGPASSWORD" {
			values[key] = value
		}
	}
	for key, value := range additions {
		values[key] = value
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

type redactor struct {
	secrets []string
	dsn     *regexp.Regexp
}

func newRedactor(config localConfig) *redactor {
	return &redactor{secrets: []string{config.migrationPassword, config.runtimePassword}, dsn: regexp.MustCompile(`(?i)postgres(?:ql)?://[^\s"'<>]+`)}
}

func (r *redactor) clean(value string) string {
	value = r.dsn.ReplaceAllString(value, "postgres://<redacted>")
	for _, secret := range r.secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "<redacted>")
		}
	}
	return value
}

func (r *redactor) line(value string) string { return r.clean(value) }

type labelWriter struct {
	mu       *sync.Mutex
	label    string
	out      io.Writer
	redactor *redactor
	buffer   bytes.Buffer
}

func (w *labelWriter) Write(data []byte) (int, error) {
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	count := len(data)
	for _, b := range data {
		if b == '\n' {
			_, _ = fmt.Fprintf(w.out, "[%s] %s\n", w.label, w.redactor.clean(w.buffer.String()))
			w.buffer.Reset()
			continue
		}
		if w.buffer.Len() < maximumOutput {
			w.buffer.WriteByte(b)
		}
	}
	return count, nil
}

func (w *labelWriter) Flush() {
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	if w.buffer.Len() != 0 {
		_, _ = fmt.Fprintf(w.out, "[%s] %s\n", w.label, w.redactor.clean(w.buffer.String()))
		w.buffer.Reset()
	}
}

type commandError struct {
	code   int
	output string
}

func (e *commandError) Error() string {
	if e.output == "" {
		return fmt.Sprintf("command exited with status %d", e.code)
	}
	return fmt.Sprintf("command exited with status %d: %s", e.code, e.output)
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > b.limit {
		remaining := b.limit - b.Len()
		if remaining > 0 {
			_, _ = b.Buffer.Write(data[:remaining])
		}
		b.exceeded = true
		return len(data), nil
	}
	return b.Buffer.Write(data)
}
