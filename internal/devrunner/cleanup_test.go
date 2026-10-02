package devrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteCleanupRetainsVolumeAndRemovesOnlyVerifiedResources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, "cleanup-executor")
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupState(StatusOptions{Project: identity.Project, WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	containerID := strings.Repeat("a", 64)
	containerName := identity.Project + "-db-aabbccddeeff"
	networkID := strings.Repeat("b", 64)
	resources := []ResourceSnapshot{
		{Kind: ResourceDatabase, Name: containerName, ID: containerID, State: ResourceRunning, Labels: state.Resources[ResourceDatabase].Labels},
		{Kind: ResourceDatabaseNetwork, Name: state.Resources[ResourceDatabaseNetwork].Name, ID: networkID, State: ResourcePresent, Labels: state.Resources[ResourceDatabaseNetwork].Labels},
		{Kind: ResourceDatabaseVolume, Name: state.Resources[ResourceDatabaseVolume].Name, State: ResourcePresent, Labels: state.Resources[ResourceDatabaseVolume].Labels},
	}
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(context.Context, privateProjectState) ([]ResourceSnapshot, error) {
		return resources, nil
	}}
	var removed []string
	command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case len(args) >= 2 && args[0] == "inspect" && args[len(args)-1] == containerID:
			labels, _ := json.Marshal(state.Resources[ResourceDatabase].Labels)
			return []byte(string(labels) + "\trunning\t" + containerName + "\t" + containerID), nil
		case len(args) == 5 && args[0] == "network" && args[1] == "inspect":
			labels, _ := json.Marshal(state.Resources[ResourceDatabaseNetwork].Labels)
			return []byte(string(labels) + "\t" + networkID), nil
		case len(args) == 3 && args[0] == "rm" && args[1] == "--force":
			removed = append(removed, joined)
		case len(args) == 3 && args[0] == "network" && args[1] == "rm":
			removed = append(removed, joined)
		default:
			return nil, errors.New("unexpected cleanup command")
		}
		return nil, nil
	}
	result, err := executeCleanup(ctx, options, false, command, func(int) error { return errors.New("unexpected process signal") }, defaultProcessProbe)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 3 || result.Plan.Actions[len(result.Plan.Actions)-1].Operation != "retain-volume" {
		t.Fatalf("cleanup did not report retained volume: %+v", result)
	}
	if len(removed) != 2 || removed[0] != "rm --force "+containerID || removed[1] != "network rm "+networkID {
		t.Fatalf("cleanup did not remove by immutable identifiers: %v", removed)
	}
}

func TestExecuteCleanupRefusesForeignResourceAndStaleProcessIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, "cleanup-refusal")
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupState(StatusOptions{Project: identity.Project, WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	containerID := strings.Repeat("c", 64)
	containerName := identity.Project + "-db-ccddeeff0011"
	foreignLabels, _ := ResourceLabels(identity, ResourceDatabase)
	foreignLabels[projectIDLabel] = "00000000-0000-4000-8000-000000000000"
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(context.Context, privateProjectState) ([]ResourceSnapshot, error) {
		return []ResourceSnapshot{{Kind: ResourceDatabase, Name: containerName, ID: containerID, State: ResourceRunning, Labels: state.Resources[ResourceDatabase].Labels}}, nil
	}}
	removals := 0
	command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "inspect" {
			encoded, _ := json.Marshal(foreignLabels)
			return []byte(string(encoded) + "\trunning\t" + containerName + "\t" + containerID), nil
		}
		if args[0] == "rm" {
			removals++
		}
		return nil, nil
	}
	if _, err := executeCleanup(ctx, options, false, command, func(int) error { return nil }, defaultProcessProbe); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("foreign resource was not refused: %v", err)
	}
	if removals != 0 {
		t.Fatal("foreign resource was removed")
	}

	stale := ProcessIdentity{PID: os.Getpid(), StartSeconds: 1, StartMicroseconds: 0, Executable: "/stale", OwnedRoot: state.Root}
	if err := storeCleanupProcess(root, identity.Project, stale); err != nil {
		t.Fatal(err)
	}
	action := CleanupAction{Operation: "stop-process", Kind: "application", Name: "owned-application", Process: &stale}
	signals := 0
	probe := func(context.Context, int, string, string) (ProcessIdentity, error) {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return ProcessIdentity{}, executableErr
		}
		workingDir, workingDirErr := os.Getwd()
		if workingDirErr != nil {
			return ProcessIdentity{}, workingDirErr
		}
		actual, err := defaultProcessProbe(ctx, os.Getpid(), executable, workingDir)
		if err != nil {
			return ProcessIdentity{}, err
		}
		actual.OwnedRoot = state.Root
		if actual.BootID != "" {
			actual.StartTicks++
		} else {
			actual.StartSeconds++
		}
		return actual, nil
	}
	cleanupErr := executeCleanupAction(ctx, options, identity, action, false, command, func(int) error { signals++; return nil }, probe)
	if signals != 0 {
		t.Fatal("stale PID cleanup sent a signal")
	}
	if !errors.Is(cleanupErr, ErrProcessIdentityMismatch) || os.Getpid() < 2 {
		t.Fatalf("reused PID was not refused: %v", cleanupErr)
	}
}

func TestExecuteCleanupStopsExactOwnedLocalProcess(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, "cleanup-process")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sleep", "30")
	command.Dir = root
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- command.Wait() }()
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			<-waitResult
		}
	}()
	identityRecord, err := RecordOwnedProcess(ctx, root, identity.Project, command.Process.Pid, command.Path)
	if err != nil {
		t.Fatal(err)
	}
	resources := []ResourceSnapshot{
		{Kind: ResourceDatabase, Name: identity.Project + "-db-*", State: ResourceMissing},
		{Kind: ResourceDatabaseNetwork, Name: identity.Project + "-database", State: ResourceMissing},
		{Kind: ResourceDatabaseVolume, Name: identity.Project + "-postgres-data", State: ResourceMissing},
	}
	options := StatusOptions{Project: identity.Project, WorkingDir: root, resourceProbe: func(context.Context, privateProjectState) ([]ResourceSnapshot, error) { return resources, nil }}
	result, err := ExecuteCleanup(ctx, options, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 1 || result.Completed[0].Operation != "stop-process" {
		t.Fatalf("owned process cleanup was not reported: %+v", result.Completed)
	}
	if err := <-waitResult; err == nil {
		t.Fatal("interrupted local process unexpectedly exited successfully")
	}
	if _, err := os.Stat(filepath.Join(root, ".amos", "devrunner", stateFileName)); err != nil {
		t.Fatal(err)
	}
	if !validProcessIdentity(identityRecord) {
		t.Fatal("test did not record a complete immutable process identity")
	}
}

func storeCleanupProcess(root, project string, identity ProcessIdentity) error {
	store, err := newPrivateStateStore(root, project, false)
	if err != nil {
		return err
	}
	state, err := store.load()
	if err != nil {
		return err
	}
	state.Process = &processRecord{Identity: identity}
	return store.write(state)
}

func TestCleanupRefusesNetworkReplacementAfterPlanning(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, "network-generation")
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupState(StatusOptions{Project: identity.Project, WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	action := CleanupAction{Operation: "remove-network", Kind: ResourceDatabaseNetwork, Name: state.Resources[ResourceDatabaseNetwork].Name, ID: strings.Repeat("b", 64)}
	removals := 0
	command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "inspect" {
			labels, _ := json.Marshal(state.Resources[ResourceDatabaseNetwork].Labels)
			return []byte(string(labels) + "\t" + strings.Repeat("c", 64)), nil
		}
		removals++
		return nil, nil
	}
	err = executeCleanupAction(ctx, StatusOptions{Project: identity.Project, WorkingDir: root}, identity, action, false, command, defaultCleanupSignal, defaultProcessProbe)
	if !errors.Is(err, ErrResourceConflict) || removals != 0 {
		t.Fatal("replacement network generation was removed")
	}
}

func TestCleanupRefusesNewRunRecordedAfterPlanning(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	identity, err := InitializeProject(ctx, root, "cleanup-new-run")
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadCleanupState(StatusOptions{Project: identity.Project, WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	live := ProcessIdentity{PID: os.Getpid(), StartSeconds: 1, Executable: "/owned-process", OwnedRoot: state.Root}
	if err := storeCleanupProcess(root, identity.Project, live); err != nil {
		t.Fatal(err)
	}
	action := CleanupAction{Operation: "remove-volume", Kind: ResourceDatabaseVolume, Name: state.Resources[ResourceDatabaseVolume].Name}
	mutations := 0
	command := func(context.Context, string, ...string) ([]byte, error) { mutations++; return nil, nil }
	probe := func(context.Context, int, string, string) (ProcessIdentity, error) { return live, nil }
	err = executeCleanupAction(ctx, StatusOptions{Project: identity.Project, WorkingDir: root}, identity, action, true, command, defaultCleanupSignal, probe)
	if !errors.Is(err, ErrProcessIdentityMismatch) || mutations != 0 {
		t.Fatal("new live run lost its resources")
	}
}
