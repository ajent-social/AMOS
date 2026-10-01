package devrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStatusPrivateIdentityPersistsAndValidatesResourceLabels(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "status-fixture")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := InitializeProject(context.Background(), root, "status-fixture")
	if err != nil || repeated != identity {
		t.Fatalf("project identity changed on restart: first=%+v second=%+v err=%v", identity, repeated, err)
	}
	statePath := filepath.Join(root, ".amos", "devrunner", stateFileName)
	for path, want := range map[string]os.FileMode{
		filepath.Dir(statePath): 0700,
		statePath:               0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("private mode for %q = %o, want %o", filepath.Base(path), info.Mode().Perm(), want)
		}
	}
	store, err := newPrivateStateStore(root, "status-fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ResourceKind{ResourceDatabase, ResourceDatabaseNetwork, ResourceDatabaseVolume} {
		labels, err := ResourceLabels(identity, kind)
		if err != nil {
			t.Fatal(err)
		}
		if !exactLabels(labels, state.Resources[kind].Labels) || state.Resources[kind].Labels[projectIDLabel] != identity.ID.String() {
			t.Fatalf("private resource identity labels were not persisted for %q", kind)
		}
	}
	if _, err := InitializeProject(context.Background(), root, "another-fixture"); !errors.Is(err, ErrProjectIdentityMismatch) {
		t.Fatalf("same directory accepted another project identity: %v", err)
	}
}

func TestStatusConcurrentFirstInitializationUsesOneIdentity(t *testing.T) {
	const callers = 8
	projectRoot := t.TempDir()
	ready := make(chan struct{}, callers)
	release := make(chan struct{})
	results := make(chan ProjectIdentity, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			identity, err := initializeProject(context.Background(), projectRoot, "parallel-first-init", func() {
				ready <- struct{}{}
				<-release
			})
			results <- identity
			errs <- err
		}()
	}
	for range callers {
		<-ready
	}
	close(release)
	wg.Wait()
	close(results)
	close(errs)
	var winner uuid.UUID
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent initialization failed: %v", err)
		}
	}
	for identity := range results {
		if winner == uuid.Nil {
			winner = identity.ID
		}
		if identity.ID != winner || identity.Project != "parallel-first-init" {
			t.Fatalf("callers observed distinct project identities: winner=%s caller=%+v", winner, identity)
		}
	}
	store, err := newPrivateStateStore(projectRoot, "parallel-first-init", false)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.load()
	if err != nil || persisted.Identity.ID != winner {
		t.Fatalf("persisted identity disagrees with initialization callers: state=%+v err=%v", persisted.Identity, err)
	}
}

func TestStatusConcurrentCrossProcessInitializationUsesOneIdentity(t *testing.T) {
	if os.Getenv("AMOS_T7_7_INIT_WORKER") == "1" {
		root := os.Getenv("AMOS_T7_7_INIT_ROOT")
		identity, err := initializeProject(context.Background(), root, "parallel-cross-process", func() {
			if _, err := fmt.Fprintln(os.Stdout, "READY"); err != nil {
				t.Fatalf("signal parent readiness: %v", err)
			}
			if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
				t.Fatalf("waiting for parent release: %v", err)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(os.Stdout, identity.ID.String()); err != nil {
			t.Fatalf("write resulting identity: %v", err)
		}
		return
	}

	root := t.TempDir()
	type worker struct {
		command *exec.Cmd
		stdin   io.WriteCloser
		stdout  *bufio.Reader
		waited  bool
	}
	workers := make([]worker, 0, 2)
	t.Cleanup(func() {
		for index := range workers {
			if workers[index].waited || workers[index].command.Process == nil {
				continue
			}
			_ = workers[index].command.Process.Kill()
			_ = workers[index].command.Wait()
		}
	})
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		t.Cleanup(cancel)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStatusConcurrentCrossProcessInitializationUsesOneIdentity$")
		command.Env = append(os.Environ(), "AMOS_T7_7_INIT_WORKER=1", "AMOS_T7_7_INIT_ROOT="+root)
		stdin, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		workers = append(workers, worker{command: command, stdin: stdin, stdout: bufio.NewReader(stdout)})
	}
	for index := range workers {
		worker := &workers[index]
		line, err := worker.stdout.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "READY" {
			t.Fatalf("worker did not pause after observing missing state: line=%q err=%v", line, err)
		}
	}
	for index := range workers {
		worker := &workers[index]
		if _, err := io.WriteString(worker.stdin, "continue\n"); err != nil {
			t.Fatal(err)
		}
		if err := worker.stdin.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var winner uuid.UUID
	for index := range workers {
		worker := &workers[index]
		line, err := worker.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("read worker identity: %v", err)
		}
		identity, err := uuid.Parse(strings.TrimSpace(line))
		if err != nil {
			t.Fatalf("worker returned invalid identity %q: %v", line, err)
		}
		if winner == uuid.Nil {
			winner = identity
		}
		if identity != winner {
			t.Fatalf("processes returned different project identities: %s and %s", winner, identity)
		}
		waitErr := worker.command.Wait()
		worker.waited = true
		if waitErr != nil {
			t.Fatalf("initialization worker failed: %v", waitErr)
		}
	}
	store, err := newPrivateStateStore(root, "parallel-cross-process", false)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.load()
	if err != nil || persisted.Identity.ID != winner {
		t.Fatalf("persisted identity disagrees with processes: state=%+v err=%v", persisted.Identity, err)
	}
}

func TestStatusConcurrentClearAndRecordKeepsNewIdentity(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "clear-record-race"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "clear-record-race", false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(projectRoot, "app")
	if err := os.WriteFile(executable, []byte("test-owned executable"), 0700); err != nil {
		t.Fatal(err)
	}
	oldIdentity := ProcessIdentity{PID: 61001, StartSeconds: 100, StartMicroseconds: 1, Executable: executable, OwnedRoot: projectRoot}
	newIdentity := ProcessIdentity{PID: 61002, StartSeconds: 200, StartMicroseconds: 2, Executable: executable, OwnedRoot: projectRoot}
	state.Process = &processRecord{Identity: oldIdentity}
	if err := store.write(state); err != nil {
		t.Fatal(err)
	}

	clearChecked := make(chan struct{})
	releaseClear := make(chan struct{})
	clearResult := make(chan error, 1)
	go func() {
		clearResult <- clearOwnedProcess(context.Background(), projectRoot, "clear-record-race", oldIdentity, processUpdateHooks{
			afterClearCheck: func() {
				close(clearChecked)
				<-releaseClear
			},
		})
	}()
	select {
	case <-clearChecked:
	case <-time.After(stateLockWait + time.Second):
		t.Fatal("clear did not reach the held-update regression point")
	}
	recordWaiting := make(chan struct{}, 1)
	type recordOutcome struct {
		identity ProcessIdentity
		err      error
	}
	recordResult := make(chan recordOutcome, 1)
	probe := func(_ context.Context, pid int, exe, root string) (ProcessIdentity, error) {
		if pid != newIdentity.PID || exe != newIdentity.Executable || root != newIdentity.OwnedRoot {
			return ProcessIdentity{}, ErrProcessIdentityMismatch
		}
		return newIdentity, nil
	}
	go func() {
		identity, recordErr := recordOwnedProcess(context.Background(), projectRoot, "clear-record-race", newIdentity.PID, executable, probe, processUpdateHooks{
			onRecordWait: func() {
				select {
				case recordWaiting <- struct{}{}:
				default:
				}
			},
		})
		recordResult <- recordOutcome{identity: identity, err: recordErr}
	}()
	var earlyRecord *recordOutcome
	select {
	case <-recordWaiting:
	case outcome := <-recordResult:
		earlyRecord = &outcome
	case <-time.After(stateLockWait + time.Second):
		close(releaseClear)
		t.Fatal("record did not block on the held process-update lock")
	}
	close(releaseClear)
	if err := <-clearResult; err != nil {
		t.Fatalf("clear of the old exact identity failed: %v", err)
	}
	var outcome recordOutcome
	if earlyRecord != nil {
		outcome = *earlyRecord
	} else {
		outcome = <-recordResult
	}
	if outcome.err != nil || !sameProcessIdentity(outcome.identity, newIdentity) {
		t.Fatalf("record of the new identity failed: identity=%+v err=%v", outcome.identity, outcome.err)
	}
	persisted, err := store.load()
	if err != nil || persisted.Process == nil || !sameProcessIdentity(persisted.Process.Identity, newIdentity) {
		t.Fatalf("new process identity was lost: process=%+v err=%v", persisted.Process, err)
	}
}

func TestPrivateStateUpdateLockIsPrivateStableAndCancellable(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "update-lock-fixture"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "update-lock-fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	firstRoot, releaseFirst, err := store.lockProcessUpdate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	firstInfo, err := firstRoot.Lstat(stateLockName)
	if err != nil || firstInfo.Mode().Perm() != 0600 || !firstInfo.Mode().IsRegular() || !ownedByCurrentUser(firstInfo) {
		releaseFirst()
		t.Fatalf("lock file is not a private owned regular file: info=%v err=%v", firstInfo, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, releaseSecond, err := store.lockProcessUpdate(ctx, nil); err == nil {
		releaseSecond()
		releaseFirst()
		t.Fatal("contended lock ignored caller cancellation")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		releaseFirst()
		t.Fatalf("lock wait did not honor bounded context: %s", elapsed)
	}
	releaseFirst()
	secondRoot, releaseSecond, err := store.lockProcessUpdate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	secondInfo, err := secondRoot.Lstat(stateLockName)
	if err != nil || !os.SameFile(firstInfo, secondInfo) {
		t.Fatalf("lock inode changed between updates: first=%v second=%v err=%v", firstInfo, secondInfo, err)
	}
}

func TestPrivateStateUpdateLockRejectsSymlink(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "update-lock-symlink"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "update-lock-symlink", false)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(projectRoot, ".amos", "devrunner", stateLockName)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatal(err)
	}
	if _, release, err := store.lockProcessUpdate(context.Background(), nil); err == nil {
		release()
		t.Fatal("symlink lock file was followed")
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "untouched" {
		t.Fatalf("symlink target was changed: contents=%q err=%v", contents, err)
	}
}

func TestPrivateStateUpdateLockRejectsIncorrectMode(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "update-lock-mode"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "update-lock-mode", false)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(projectRoot, ".amos", "devrunner", stateLockName)
	if err := os.WriteFile(lockPath, nil, 0640); err != nil {
		t.Fatal(err)
	}
	if _, release, err := store.lockProcessUpdate(context.Background(), nil); err == nil {
		release()
		t.Fatal("group-readable update lock was accepted")
	}
}

func TestPrivateStateUpdateLockRejectsHardLink(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "update-lock-hardlink"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "update-lock-hardlink", false)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(projectRoot, ".amos", "devrunner", stateFileName)
	lockPath := filepath.Join(projectRoot, ".amos", "devrunner", stateLockName)
	if err := os.Link(statePath, lockPath); err != nil {
		t.Skipf("filesystem does not permit isolated hard-link test: %v", err)
	}
	if _, release, err := store.lockProcessUpdate(context.Background(), nil); err == nil {
		release()
		t.Fatal("hard-linked project state was accepted as the update lock")
	}
}

func TestPrivateStateUpdateLockRejectsForeignOwner(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("requires Linux root to create a foreign-owned test lock")
	}
	projectRoot := t.TempDir()
	if _, err := InitializeProject(context.Background(), projectRoot, "update-lock-foreign-owner"); err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(projectRoot, "update-lock-foreign-owner", false)
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := store.lockProcessUpdate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
	lockPath := filepath.Join(projectRoot, ".amos", "devrunner", stateLockName)
	if err := os.Chown(lockPath, 65534, 65534); err != nil {
		t.Skipf("cannot change ownership of isolated test lock: %v", err)
	}
	if _, release, err := store.lockProcessUpdate(context.Background(), nil); err == nil {
		release()
		t.Fatal("foreign-owned update lock was accepted")
	}
}

func TestPrivateStateDirectoryRejectsChangedInode(t *testing.T) {
	project := t.TempDir()
	if _, err := InitializeProject(context.Background(), project, "directory-race-fixture"); err != nil {
		t.Fatal(err)
	}
	projectRoot, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = projectRoot.Close() }()
	amosRoot, err := openPinnedDirectory(projectRoot, ".amos", false, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = amosRoot.Close() }()
	_, err = openPinnedDirectory(amosRoot, "devrunner", false, 0700, func() error {
		if err := amosRoot.Rename("devrunner", "devrunner-original"); err != nil {
			return err
		}
		return amosRoot.Mkdir("devrunner", 0700)
	})
	if !errors.Is(err, ErrProjectStateUnavailable) {
		t.Fatalf("different directory inode between Lstat and OpenRoot was accepted: %v", err)
	}
}

func TestPrivateStateReadRejectsChangedFileInode(t *testing.T) {
	project := t.TempDir()
	identity, err := InitializeProject(context.Background(), project, "file-race-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(project, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.openStateRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	_, err = readPrivateStateFile(root, stateFileName, func() error {
		if err := root.Rename(stateFileName, "state.json-original"); err != nil {
			return err
		}
		file, err := root.OpenFile(stateFileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write([]byte("replacement"))
		closeErr := file.Close()
		return errors.Join(writeErr, closeErr)
	})
	if !errors.Is(err, ErrProjectStateUnavailable) {
		t.Fatalf("replaced state file inode was read: %v", err)
	}
}

func TestPrivateStateReadRejectsSameInodeSymlinkReplacement(t *testing.T) {
	project := t.TempDir()
	identity, err := InitializeProject(context.Background(), project, "file-symlink-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(project, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.openStateRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	_, err = readPrivateStateFile(root, stateFileName, func() error {
		if err := root.Rename(stateFileName, "state.json-original"); err != nil {
			return err
		}
		return os.Symlink(filepath.Join(project, ".amos", "devrunner", "state.json-original"), filepath.Join(project, ".amos", "devrunner", stateFileName))
	})
	if !errors.Is(err, ErrProjectStateUnavailable) {
		t.Fatalf("same-inode symlink replacement was accepted: %v", err)
	}
}

func TestPrivateStateWriteStaysWithinPinnedDirectoryAfterPathSwap(t *testing.T) {
	project := t.TempDir()
	identity, err := InitializeProject(context.Background(), project, "write-race-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(project, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := store.openStateRoot(false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	amos, err := os.OpenRoot(filepath.Join(project, ".amos"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = amos.Close() }()
	data := []byte("pinned-write")
	err = writePrivateStateFile(pinned, stateFileName, data, func() error {
		if err := amos.Rename("devrunner", "devrunner-original"); err != nil {
			return err
		}
		return amos.Mkdir("devrunner", 0700)
	})
	if err != nil {
		t.Fatalf("pinned write failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".amos", "devrunner", stateFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("write followed replacement path: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(project, ".amos", "devrunner-original", stateFileName))
	if err != nil || string(written) != string(data) {
		t.Fatalf("pinned directory did not receive atomic write: %q err=%v", written, err)
	}
}

func TestProcessStartIdentityRejectsPIDReuseDuringProbe(t *testing.T) {
	before := processStartIdentity{PID: 45125, StartSeconds: 900, StartMicroseconds: 123}
	if !sameDarwinProcessProbe(before, 501, before, 501, 501) {
		t.Fatal("stable kernel process token was rejected")
	}
	after := before
	after.StartSeconds++
	if sameDarwinProcessProbe(before, 501, after, 501, 501) {
		t.Fatal("PID reuse during process path probe was accepted")
	}
	after = before
	after.StartMicroseconds++
	if sameDarwinProcessProbe(before, 501, after, 501, 501) {
		t.Fatal("PID reuse within a start second during process path probe was accepted")
	}
	if sameDarwinProcessProbe(before, 501, before, 502, 501) || sameDarwinProcessProbe(before, 502, before, 502, 501) {
		t.Fatal("changed or non-current process UID during path probe was accepted")
	}
}

func TestStatusAndCleanupExcludeStalePIDReuse(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "stale-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	wanted := ProcessIdentity{PID: 45123, StartSeconds: 100, StartMicroseconds: 123, Executable: filepath.Join(root, "app"), OwnedRoot: root}
	state.Process = &processRecord{Identity: wanted}
	if err := store.write(state); err != nil {
		t.Fatal(err)
	}
	reusedPID := wanted
	reusedPID.StartSeconds++
	options := StatusOptions{
		Project: identity.Project, WorkingDir: root,
		processProbe: func(_ context.Context, pid int, executable, ownedRoot string) (ProcessIdentity, error) {
			if pid != wanted.PID || executable != wanted.Executable || ownedRoot != wanted.OwnedRoot {
				t.Fatalf("probe did not receive exact persisted process identity")
			}
			return reusedPID, nil
		},
		resourceProbe: func(_ context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
			return fixtureResources(state, ResourceMissing), nil
		},
	}
	status, err := InspectProject(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if status.Process.State != ResourceStopped || status.State != ProjectStopped {
		t.Fatalf("stale PID reuse was reported as owned/running: %+v", status)
	}
	plan, err := PlanCleanup(context.Background(), options, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Operation == "stop-process" {
			t.Fatalf("cleanup plan targeted reused PID: %+v", action)
		}
	}
}

func TestCleanupProcessActionCarriesExactIdentityForRevalidation(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "cleanup-process-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	wanted := ProcessIdentity{PID: 45124, StartSeconds: 321, StartMicroseconds: 456, Executable: filepath.Join(root, "app"), OwnedRoot: root}
	state.Process = &processRecord{Identity: wanted}
	if err := store.write(state); err != nil {
		t.Fatal(err)
	}
	options := StatusOptions{
		Project: identity.Project, WorkingDir: root,
		processProbe: func(_ context.Context, pid int, executable, ownedRoot string) (ProcessIdentity, error) {
			if pid != wanted.PID || executable != wanted.Executable || ownedRoot != wanted.OwnedRoot {
				t.Fatalf("probe did not receive exact persisted process identity")
			}
			return wanted, nil
		},
		resourceProbe: func(_ context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
			return fixtureResources(state, ResourceMissing), nil
		},
	}
	plan, err := PlanCleanup(context.Background(), options, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Operation != "stop-process" {
			continue
		}
		if action.Process == nil || *action.Process != wanted || action.ID != "" {
			t.Fatalf("stop action did not carry full process authority: %+v", action)
		}
		return
	}
	t.Fatalf("cleanup plan omitted the verified live process action: %+v", plan)
}

func TestCleanPlanRetainsDataUnlessExplicitlyRequested(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "cleanup-fixture")
	if err != nil {
		t.Fatal(err)
	}
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(_ context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
		resources := fixtureResources(state, ResourcePresent)
		resources[0] = ResourceSnapshot{Kind: ResourceDatabase, Name: "cleanup-fixture-db-aabbccddeeff", ID: strings.Repeat("a", 64), State: ResourceRunning, Labels: state.Resources[ResourceDatabase].Labels}
		return resources, nil
	}}
	defaultPlan, err := PlanCleanup(context.Background(), options, false)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := map[string]bool{"remove-container": false, "remove-network": false, "retain-volume": false, "remove-volume": false}
	for _, action := range defaultPlan.Actions {
		if _, ok := wantDefault[action.Operation]; !ok {
			t.Fatalf("unexpected default cleanup operation: %+v", action)
		}
		wantDefault[action.Operation] = true
	}
	for operation, seen := range wantDefault {
		if !seen && operation != "remove-volume" {
			t.Fatalf("default cleanup omitted %q: %+v", operation, defaultPlan.Actions)
		}
	}
	if wantDefault["remove-volume"] || !wantDefault["retain-volume"] {
		t.Fatalf("default cleanup did not retain data: %+v", defaultPlan.Actions)
	}
	deletePlan, err := PlanCleanup(context.Background(), options, true)
	if err != nil {
		t.Fatal(err)
	}
	foundDelete, foundRetain := false, false
	for _, action := range deletePlan.Actions {
		foundDelete = foundDelete || action.Operation == "remove-volume"
		foundRetain = foundRetain || action.Operation == "retain-volume"
	}
	if !foundDelete || foundRetain {
		t.Fatalf("explicit delete-data plan mismatch: %+v", deletePlan.Actions)
	}
}

func TestStatusUnknownProcessProbeProducesNoCleanupOperations(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "unknown-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	state.Process = &processRecord{Identity: ProcessIdentity{PID: 199, StartSeconds: 200, Executable: "/tmp/app", OwnedRoot: root}}
	if err := store.write(state); err != nil {
		t.Fatal(err)
	}
	options := StatusOptions{Project: identity.Project, WorkingDir: root,
		processProbe: func(context.Context, int, string, string) (ProcessIdentity, error) {
			return ProcessIdentity{}, errProcessProbeUnavailable
		},
		resourceProbe: func(_ context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
			return fixtureResources(state, ResourcePresent), nil
		},
	}
	status, err := InspectProject(context.Background(), options)
	if err != nil || status.State != ProjectUnknown || status.Process.State != ResourceUnknown {
		t.Fatalf("unavailable process probe did not yield unknown: status=%+v err=%v", status, err)
	}
	plan, err := PlanCleanup(context.Background(), options, true)
	if err != nil || len(plan.Actions) != 0 {
		t.Fatalf("unknown project generated destructive cleanup actions: plan=%+v err=%v", plan, err)
	}
}

func TestStatusPodmanRequiresFullProjectIDAndResourceLabels(t *testing.T) {
	root := t.TempDir()
	identity, err := InitializeProject(context.Background(), root, "podman-fixture")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newPrivateStateStore(root, identity.Project, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	foreignID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	foreignLabels := map[string]string{projectLabel: identity.Project, projectIDLabel: foreignID.String(), resourceLabel: string(ResourceDatabase)}
	containerID := strings.Repeat("b", 64)
	containerName := identity.Project + "-db-aabbccddeeff"
	podman := filepath.Join(t.TempDir(), "fake-podman")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
case "$1" in
  ps) printf '%%s\\n' '%s'; exit 0 ;;
  inspect) printf '%%s\\t%%s\\t%%s\\t%%s\\n' '%s' 'running' '/%s' '%s'; exit 0 ;;
  network|volume) printf '%%s\\n' '{}'; exit 0 ;;
esac
exit 2
`, containerID, jsonEscapeForShell(t, foreignLabels), containerName, containerID)
	if err := os.WriteFile(podman, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	options := StatusOptions{Project: identity.Project, WorkingDir: root, PodmanBinary: podman,
		processProbe: func(context.Context, int, string, string) (ProcessIdentity, error) {
			return ProcessIdentity{}, errProcessNotFound
		},
	}
	resources, err := inspectPodmanResources(context.Background(), state, podman)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.State == ResourceRunning || resource.State == ResourcePresent {
			t.Fatalf("foreign or unlabeled resource was accepted as owned: %+v", resource)
		}
	}
	options.resourceProbe = func(ctx context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
		return inspectPodmanResources(ctx, state, podman)
	}
	plan, err := PlanCleanup(context.Background(), options, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("foreign/unlabeled Podman resources entered cleanup plan: %+v", plan.Actions)
	}
}

func fixtureResources(state privateProjectState, namedState ResourceState) []ResourceSnapshot {
	return []ResourceSnapshot{
		{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-*", State: namedState, Labels: state.Resources[ResourceDatabase].Labels},
		{Kind: ResourceDatabaseNetwork, Name: state.Resources[ResourceDatabaseNetwork].Name, State: namedState, Labels: state.Resources[ResourceDatabaseNetwork].Labels},
		{Kind: ResourceDatabaseVolume, Name: state.Resources[ResourceDatabaseVolume].Name, State: namedState, Labels: state.Resources[ResourceDatabaseVolume].Labels},
	}
}

func jsonEscapeForShell(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "'", "'\\''")
}
