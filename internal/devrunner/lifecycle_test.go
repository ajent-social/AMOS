package devrunner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycleLockExcludesSecondStartAndCleanupPlanning(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "lifecycle-contention")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	_, releaseStartup, err := store.lockLifecycle(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if releaseStartup != nil {
			releaseStartup()
		}
	}()

	secondWaiting := make(chan struct{}, 1)
	secondAcquired := make(chan func(), 1)
	go func() {
		_, release, lockErr := store.lockLifecycle(context.Background(), func() {
			select {
			case secondWaiting <- struct{}{}:
			default:
			}
		})
		if lockErr != nil {
			secondAcquired <- nil
			return
		}
		secondAcquired <- release
	}()
	select {
	case <-secondWaiting:
	case <-time.After(time.Second):
		t.Fatal("second startup did not contend for the project lifecycle lock")
	}
	select {
	case release := <-secondAcquired:
		if release != nil {
			release()
		}
		t.Fatal("second startup acquired the project lifecycle lock concurrently")
	case <-time.After(30 * time.Millisecond):
	}

	var planned atomic.Int32
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(context.Context, privateProjectState) ([]ResourceSnapshot, error) {
		planned.Add(1)
		return nil, nil
	}}
	cleanupDone := make(chan error, 1)
	go func() {
		_, cleanupErr := executeCleanup(context.Background(), options, false,
			func(context.Context, string, ...string) ([]byte, error) {
				return nil, errors.New("unexpected cleanup mutation")
			},
			func(int) error { return errors.New("unexpected process signal") }, defaultProcessProbe)
		cleanupDone <- cleanupErr
	}()
	time.Sleep(30 * time.Millisecond)
	if planned.Load() != 0 {
		t.Fatal("cleanup planned resources while startup held the lifecycle lock")
	}

	releaseStartup()
	releaseStartup = nil
	select {
	case release := <-secondAcquired:
		if release == nil {
			t.Fatal("second startup failed after the first startup released the lock")
		}
		release()
	case <-time.After(time.Second):
		t.Fatal("second startup remained excluded after lock release")
	}
	select {
	case cleanupErr := <-cleanupDone:
		if cleanupErr != nil {
			t.Fatalf("cleanup failed after startup released the lock: %v", cleanupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not proceed after startup released the lock")
	}
	if planned.Load() != 1 {
		t.Fatalf("cleanup plan count = %d, want 1 after startup", planned.Load())
	}
}

func TestCleanupCancellationWhileLifecycleBusyHasNoEffects(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "lifecycle-cancel")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := store.lockLifecycle(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	var planned, mutated atomic.Int32
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(context.Context, privateProjectState) ([]ResourceSnapshot, error) {
		planned.Add(1)
		return nil, nil
	}}
	done := make(chan error, 1)
	go func() {
		_, cleanupErr := executeCleanup(ctx, options, false,
			func(context.Context, string, ...string) ([]byte, error) { mutated.Add(1); return nil, nil },
			func(int) error { mutated.Add(1); return nil }, defaultProcessProbe)
		done <- cleanupErr
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case cleanupErr := <-done:
		if cleanupErr == nil {
			t.Fatal("cleanup succeeded after cancellation while waiting for startup")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop waiting after cancellation")
	}
	if planned.Load() != 0 || mutated.Load() != 0 {
		t.Fatalf("cancelled cleanup had effects: planned=%d mutated=%d", planned.Load(), mutated.Load())
	}
}

func TestDeferredRunCleanupRefusesNewLiveProcess(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "lifecycle-live-process")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "30")
	child.Dir = root
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()
	t.Cleanup(func() {
		select {
		case <-waited:
			return
		default:
		}
		_ = child.Process.Kill()
		<-waited
	})
	if _, err := RecordOwnedProcess(context.Background(), root, identity.Project, child.Process.Pid, child.Path); err != nil {
		t.Fatal(err)
	}
	resources := &resources{identity: identity, options: Options{Project: identity.Project, WorkingDir: root}}
	if err := resources.cleanup(); !errors.Is(err, ErrProcessIdentityMismatch) {
		t.Fatalf("deferred cleanup accepted a newly registered process: %v", err)
	}
	state, err := loadCleanupState(StatusOptions{Project: identity.Project, WorkingDir: root})
	if err != nil || state.Process == nil {
		t.Fatal("deferred cleanup removed the new process record")
	}
	if _, err := defaultProcessProbe(context.Background(), state.Process.Identity.PID, state.Process.Identity.Executable, state.Process.Identity.OwnedRoot); err != nil {
		t.Fatal("deferred cleanup stopped the new process")
	}
}

func TestDeferredRunCleanupAcceptsResourcesAlreadyRemoved(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "lifecycle-already-removed")
	if err != nil {
		t.Fatal(err)
	}
	binary := root + "/podman"
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := &resources{
		identity:       identity,
		containerID:    strings.Repeat("a", 64),
		createdNetwork: true,
		networkID:      strings.Repeat("b", 64),
		options:        Options{Project: identity.Project, WorkingDir: root, PodmanBinary: binary},
	}
	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup did not accept already removed resources: %v", err)
	}
}

func TestRunRejectsExistingProcessBeforeCreatingResources(t *testing.T) {
	root := t.TempDir()
	project := "lifecycle-existing-process"
	_, err := InitializeProject(context.Background(), root, project)
	if err != nil {
		t.Fatal(err)
	}
	privateConfig := "AMOS_DB_PORT=" + freePort(t) + "\n" + `AMOS_DB_NAME=local_db
AMOS_DB_MIGRATION_USER=migrator
AMOS_DB_MIGRATION_PASSWORD=<migration-fixture>
AMOS_DB_RUNTIME_USER=runtime_user
AMOS_DB_RUNTIME_PASSWORD=<runtime-fixture>
`
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte(privateConfig), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "podman-invocations")
	podman := filepath.Join(root, "podman")
	if err := os.WriteFile(podman, []byte("#!/bin/sh\nprintf call >> \"$AMOS_DEVRUNNER_PODMAN_MARKER\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMOS_DEVRUNNER_PODMAN_MARKER", marker)
	child := exec.Command("sleep", "30")
	child.Dir = root
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()
	t.Cleanup(func() {
		select {
		case <-waited:
			return
		default:
		}
		_ = child.Process.Kill()
		<-waited
	})
	if _, err := RecordOwnedProcess(context.Background(), root, project, child.Process.Pid, child.Path); err != nil {
		t.Fatal(err)
	}
	options := Options{Project: project, WorkingDir: root, AppBinary: os.Args[0], ReadinessURL: freeURL(t), PodmanBinary: podman}
	if err := Run(context.Background(), options); !errors.Is(err, ErrProcessIdentityMismatch) {
		t.Fatalf("Run did not reject the existing application process: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Run invoked Podman before rejecting the existing process")
	}
	stateStore, err := newPrivateStateStore(root, project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := stateStore.load()
	if err != nil || state.Process == nil {
		t.Fatal("Run altered the existing process record")
	}
}
