package devrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const (
	stateVersion   = 1
	stateFileName  = "state.json"
	maxStateBytes  = 16 << 10
	maxProbeOutput = 64 << 10
	probeTimeout   = 2 * time.Second
	projectLabel   = "io.amos.project"
	projectIDLabel = "io.amos.project_id"
	resourceLabel  = "io.amos.resource"
)

var (
	ErrProjectStateUnavailable = errors.New("local project state is unavailable")
	ErrProjectIdentityMismatch = errors.New("local project identity does not match this directory")
	ErrProcessIdentityMismatch = errors.New("local process identity is not owned by this project")
	errProcessNotFound         = errors.New("owned process no longer exists")
	errProcessProbeUnavailable = errors.New("exact process identity could not be verified")
)

type ResourceKind string

const (
	ResourceDatabase        ResourceKind = "database"
	ResourceDatabaseNetwork ResourceKind = "database-network"
	ResourceDatabaseVolume  ResourceKind = "database-volume"
)

type ResourceState string

const (
	ResourceRunning ResourceState = "running"
	ResourceStopped ResourceState = "stopped"
	ResourcePresent ResourceState = "present"
	ResourceMissing ResourceState = "missing"
	ResourceUnknown ResourceState = "unknown"
)

type ProjectState string

const (
	ProjectRunning ProjectState = "running"
	ProjectStopped ProjectState = "stopped"
	ProjectPartial ProjectState = "partial"
	ProjectUnknown ProjectState = "unknown"
)

type ProjectIdentity struct {
	ID      uuid.UUID `json:"id"`
	Project string    `json:"project"`
}

// ProcessIdentity deliberately stores no argv or environment. PID is only a
// lookup hint; cleanup eligibility requires the immutable start token and the
// same executable and project root to be observed again.
type ProcessIdentity struct {
	PID               int    `json:"pid"`
	StartSeconds      int64  `json:"start_seconds"`
	StartMicroseconds int64  `json:"start_microseconds"`
	Executable        string `json:"executable"`
	OwnedRoot         string `json:"owned_root"`
}

type processStartIdentity struct {
	PID               int
	StartSeconds      int64
	StartMicroseconds int64
}

type ResourceSnapshot struct {
	Kind   ResourceKind      `json:"kind"`
	Name   string            `json:"name"`
	ID     string            `json:"id,omitempty"`
	State  ResourceState     `json:"state"`
	Labels map[string]string `json:"labels,omitempty"`
}

type ProcessSnapshot struct {
	State ResourceState `json:"state"`
	PID   int           `json:"pid,omitempty"`
}

type ProjectSnapshot struct {
	Identity  ProjectIdentity    `json:"identity"`
	State     ProjectState       `json:"state"`
	Process   ProcessSnapshot    `json:"process"`
	Resources []ResourceSnapshot `json:"resources"`
}

type StatusOptions struct {
	Project      string
	WorkingDir   string
	PodmanBinary string

	processProbe  func(context.Context, int, string, string) (ProcessIdentity, error)
	resourceProbe func(context.Context, privateProjectState) ([]ResourceSnapshot, error)
}

type CleanupAction struct {
	Operation string           `json:"operation"`
	Kind      ResourceKind     `json:"kind"`
	Name      string           `json:"name"`
	ID        string           `json:"id,omitempty"`
	Process   *ProcessIdentity `json:"process,omitempty"`
}

type CleanupPlan struct {
	Identity ProjectIdentity `json:"identity"`
	Actions  []CleanupAction `json:"actions"`
	Warnings []string        `json:"warnings,omitempty"`
}

type processRecord struct {
	Identity ProcessIdentity `json:"identity"`
}

type privateProjectState struct {
	Version   int                               `json:"version"`
	Identity  ProjectIdentity                   `json:"identity"`
	Root      string                            `json:"root"`
	Resources map[ResourceKind]ResourceSnapshot `json:"resources"`
	Process   *processRecord                    `json:"process,omitempty"`
	UpdatedAt time.Time                         `json:"updated_at"`
}

type privateStateStore struct {
	root    string
	project string
}

func InitializeProject(ctx context.Context, workingDir, project string) (ProjectIdentity, error) {
	if ctx == nil || ctx.Err() != nil {
		return ProjectIdentity{}, ErrProjectStateUnavailable
	}
	store, err := newPrivateStateStore(workingDir, project, true)
	if err != nil {
		return ProjectIdentity{}, ErrProjectStateUnavailable
	}
	state, err := store.load()
	if err == nil {
		return state.Identity, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ProjectIdentity{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return ProjectIdentity{}, ErrProjectStateUnavailable
	}
	identity := ProjectIdentity{ID: id, Project: project}
	state = newProjectState(identity, store.root)
	if err := store.write(state); err != nil {
		return ProjectIdentity{}, err
	}
	return identity, nil
}

// RecordOwnedProcess persists an exact process identity after verifying the
// current process image, kernel start token and working directory.
func RecordOwnedProcess(ctx context.Context, workingDir, project string, pid int, executable string) (ProcessIdentity, error) {
	if ctx == nil || ctx.Err() != nil || pid < 2 {
		return ProcessIdentity{}, ErrProcessIdentityMismatch
	}
	store, err := newPrivateStateStore(workingDir, project, false)
	if err != nil {
		return ProcessIdentity{}, ErrProjectStateUnavailable
	}
	state, err := store.load()
	if err != nil {
		return ProcessIdentity{}, ErrProjectStateUnavailable
	}
	root, err := canonicalPath(workingDir)
	if err != nil || root != state.Root {
		return ProcessIdentity{}, ErrProjectIdentityMismatch
	}
	executable, err = canonicalExecutablePath(executable)
	if err != nil {
		return ProcessIdentity{}, ErrProcessIdentityMismatch
	}
	probe := defaultProcessProbe
	identity, err := probe(ctx, pid, executable, root)
	if err != nil || identity.PID != pid || identity.Executable != executable || identity.OwnedRoot != root {
		return ProcessIdentity{}, ErrProcessIdentityMismatch
	}
	state.Process = &processRecord{Identity: identity}
	state.UpdatedAt = time.Now().UTC()
	if err := store.write(state); err != nil {
		return ProcessIdentity{}, err
	}
	return identity, nil
}

// ClearOwnedProcess removes only the exact process record that the caller
// previously persisted. A stale PID cannot clear a newer process identity.
func ClearOwnedProcess(ctx context.Context, workingDir, project string, expected ProcessIdentity) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrProjectStateUnavailable
	}
	store, err := newPrivateStateStore(workingDir, project, false)
	if err != nil {
		return ErrProjectStateUnavailable
	}
	state, err := store.load()
	if err != nil || state.Process == nil {
		return ErrProjectStateUnavailable
	}
	if !sameProcessIdentity(state.Process.Identity, expected) {
		return ErrProcessIdentityMismatch
	}
	state.Process = nil
	state.UpdatedAt = time.Now().UTC()
	return store.write(state)
}

func ResourceLabels(identity ProjectIdentity, kind ResourceKind) (map[string]string, error) {
	if !validProjectIdentity(identity) || !validResourceKind(kind) {
		return nil, ErrProjectIdentityMismatch
	}
	return map[string]string{
		projectLabel:   identity.Project,
		projectIDLabel: identity.ID.String(),
		resourceLabel:  string(kind),
	}, nil
}

func InspectProject(ctx context.Context, options StatusOptions) (ProjectSnapshot, error) {
	if ctx == nil || ctx.Err() != nil {
		return ProjectSnapshot{}, ErrProjectStateUnavailable
	}
	store, err := newPrivateStateStore(options.WorkingDir, options.Project, false)
	if err != nil {
		return ProjectSnapshot{State: ProjectUnknown}, nil
	}
	state, err := store.load()
	if err != nil {
		return ProjectSnapshot{State: ProjectUnknown}, nil
	}
	result := ProjectSnapshot{Identity: state.Identity, State: ProjectUnknown, Process: ProcessSnapshot{State: ResourceStopped}}
	if state.Process != nil {
		probe := options.processProbe
		if probe == nil {
			probe = defaultProcessProbe
		}
		observed, probeErr := probe(ctx, state.Process.Identity.PID, state.Process.Identity.Executable, state.Process.Identity.OwnedRoot)
		if probeErr != nil {
			if !errors.Is(probeErr, errProcessNotFound) {
				result.Process.State = ResourceUnknown
			} else {
				result.Process.State = ResourceStopped
			}
		} else if sameProcessIdentity(state.Process.Identity, observed) {
			result.Process.State = ResourceRunning
			result.Process.PID = observed.PID
		} else {
			result.Process.State = ResourceStopped
		}
	}
	resources := options.resourceProbe
	if resources == nil {
		resources = func(ctx context.Context, state privateProjectState) ([]ResourceSnapshot, error) {
			return inspectPodmanResources(ctx, state, options.PodmanBinary)
		}
	}
	snapshots, probeErr := resources(ctx, state)
	if probeErr != nil {
		return ProjectSnapshot{Identity: state.Identity, State: ProjectUnknown, Process: result.Process}, nil
	}
	result.Resources = snapshots
	result.State = combineProjectState(result.Process.State, snapshots)
	return result, nil
}

// PlanCleanup is pure: it returns exact verified operations but never executes
// them. Database data is retained unless deleteData is explicitly true.
func PlanCleanup(ctx context.Context, options StatusOptions, deleteData bool) (CleanupPlan, error) {
	status, err := InspectProject(ctx, options)
	if err != nil {
		return CleanupPlan{}, err
	}
	plan := CleanupPlan{Identity: status.Identity, Actions: make([]CleanupAction, 0, 4)}
	if status.State == ProjectUnknown || status.Identity.ID == uuid.Nil {
		plan.Warnings = []string{"project or resource ownership could not be verified"}
		return plan, nil
	}
	if status.Process.State == ResourceRunning {
		state, loadErr := newPrivateStateStore(options.WorkingDir, options.Project, false)
		if loadErr == nil {
			if persisted, e := state.load(); e == nil && persisted.Process != nil && exactLiveProcess(ctx, options, persisted.Process.Identity) {
				identity := persisted.Process.Identity
				plan.Actions = append(plan.Actions, CleanupAction{Operation: "stop-process", Kind: "application", Name: "owned-application", Process: &identity})
			} else {
				plan.Warnings = append(plan.Warnings, "process identity could not be revalidated")
			}
		}
	}
	for _, resource := range status.Resources {
		switch {
		case resource.Kind == ResourceDatabase && (resource.State == ResourceRunning || resource.State == ResourceStopped) && podmanIDRE.MatchString(resource.ID):
			plan.Actions = append(plan.Actions, CleanupAction{Operation: "remove-container", Kind: resource.Kind, Name: resource.Name, ID: resource.ID})
		case resource.Kind == ResourceDatabaseNetwork && resource.State == ResourcePresent && resource.Name != "":
			plan.Actions = append(plan.Actions, CleanupAction{Operation: "remove-network", Kind: resource.Kind, Name: resource.Name})
		case resource.Kind == ResourceDatabaseVolume && resource.State == ResourcePresent && resource.Name != "":
			if deleteData {
				plan.Actions = append(plan.Actions, CleanupAction{Operation: "remove-volume", Kind: resource.Kind, Name: resource.Name})
			} else {
				plan.Actions = append(plan.Actions, CleanupAction{Operation: "retain-volume", Kind: resource.Kind, Name: resource.Name})
			}
		case resource.State == ResourceUnknown:
			plan.Warnings = append(plan.Warnings, "one or more resources could not be verified and were excluded")
		}
	}
	return plan, nil
}

// exactLiveProcess is planning-time evidence only. An executor must repeat the
// full probe against Process immediately before signaling; the plan does not
// make a PID safe to use after its recorded process has exited.
func exactLiveProcess(ctx context.Context, options StatusOptions, expected ProcessIdentity) bool {
	probe := options.processProbe
	if probe == nil {
		probe = defaultProcessProbe
	}
	actual, err := probe(ctx, expected.PID, expected.Executable, expected.OwnedRoot)
	return err == nil && sameProcessIdentity(expected, actual)
}

func sameProcessIdentity(expected, actual ProcessIdentity) bool {
	return expected.PID >= 2 && expected.PID == actual.PID && expected.StartSeconds > 0 && expected.StartSeconds == actual.StartSeconds && expected.StartMicroseconds >= 0 && expected.StartMicroseconds == actual.StartMicroseconds && expected.Executable != "" && filepath.Clean(expected.Executable) == filepath.Clean(actual.Executable) && expected.OwnedRoot != "" && filepath.Clean(expected.OwnedRoot) == filepath.Clean(actual.OwnedRoot)
}

func sameProcessStart(expected, actual processStartIdentity) bool {
	return expected.PID >= 2 && expected.PID == actual.PID && expected.StartSeconds > 0 && expected.StartSeconds == actual.StartSeconds && expected.StartMicroseconds >= 0 && expected.StartMicroseconds == actual.StartMicroseconds && actual.StartMicroseconds < 1_000_000
}

func sameDarwinProcessProbe(expected processStartIdentity, expectedUID uint32, actual processStartIdentity, actualUID uint32, currentUID uint32) bool {
	return expectedUID == currentUID && actualUID == currentUID && sameProcessStart(expected, actual)
}

func combineProjectState(process ResourceState, resources []ResourceSnapshot) ProjectState {
	var database ResourceState
	unknown := false
	for _, resource := range resources {
		if resource.State == ResourceUnknown {
			unknown = true
		}
		if resource.Kind == ResourceDatabase {
			if database == "" || resource.State == ResourceRunning {
				database = resource.State
			} else if database != resource.State {
				database = ResourceUnknown
			}
		}
	}
	if unknown || process == ResourceUnknown || database == ResourceUnknown {
		return ProjectUnknown
	}
	if process == ResourceRunning && database == ResourceRunning {
		return ProjectRunning
	}
	if process == ResourceRunning || database == ResourceRunning {
		return ProjectPartial
	}
	return ProjectStopped
}

func newProjectState(identity ProjectIdentity, root string) privateProjectState {
	resources := map[ResourceKind]ResourceSnapshot{}
	for _, spec := range []struct {
		kind ResourceKind
		name string
	}{
		{ResourceDatabaseVolume, identity.Project + "-postgres-data"},
		{ResourceDatabaseNetwork, identity.Project + "-database"},
	} {
		labels, _ := ResourceLabels(identity, spec.kind)
		resources[spec.kind] = ResourceSnapshot{Kind: spec.kind, Name: spec.name, State: ResourceUnknown, Labels: labels}
	}
	containerLabels, _ := ResourceLabels(identity, ResourceDatabase)
	resources[ResourceDatabase] = ResourceSnapshot{Kind: ResourceDatabase, Name: identity.Project + "-db-*", State: ResourceUnknown, Labels: containerLabels}
	return privateProjectState{Version: stateVersion, Identity: identity, Root: root, Resources: resources, UpdatedAt: time.Now().UTC()}
}

func newPrivateStateStore(workingDir, project string, create bool) (*privateStateStore, error) {
	if !slugRE.MatchString(project) {
		return nil, ErrProjectStateUnavailable
	}
	root, err := canonicalPath(workingDir)
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	store := &privateStateStore{root: root, project: project}
	if create {
		pinned, err := store.openStateRoot(true)
		if err != nil {
			return nil, err
		}
		if err := pinned.Close(); err != nil {
			return nil, ErrProjectStateUnavailable
		}
	}
	return store, nil
}

func (s *privateStateStore) load() (privateProjectState, error) {
	root, err := s.openStateRoot(false)
	if err != nil {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	defer func() { _ = root.Close() }()
	data, err := readPrivateStateFile(root, stateFileName, nil)
	if errors.Is(err, os.ErrNotExist) {
		if s.verifyStateRootPath(root) != nil {
			return privateProjectState{}, ErrProjectStateUnavailable
		}
		return privateProjectState{}, err
	}
	if err != nil || s.verifyStateRootPath(root) != nil {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	var state privateProjectState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || state.Version != stateVersion || state.Root != s.root || state.Identity.Project != s.project || !validProjectIdentity(state.Identity) || !validResources(state) || state.UpdatedAt.IsZero() {
		return privateProjectState{}, ErrProjectIdentityMismatch
	}
	if state.Process != nil && (!validProcessIdentity(state.Process.Identity) || state.Process.Identity.OwnedRoot != state.Root) {
		return privateProjectState{}, ErrProjectStateUnavailable
	}
	return state, nil
}

func (s *privateStateStore) write(state privateProjectState) error {
	if s == nil || state.Version != stateVersion || state.Root != s.root || state.Identity.Project != s.project || !validProjectIdentity(state.Identity) || !validResources(state) || (state.Process != nil && (!validProcessIdentity(state.Process.Identity) || state.Process.Identity.OwnedRoot != state.Root)) {
		return ErrProjectStateUnavailable
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxStateBytes {
		return ErrProjectStateUnavailable
	}
	root, err := s.openStateRoot(false)
	if err != nil {
		return ErrProjectStateUnavailable
	}
	defer func() { _ = root.Close() }()
	if err := writePrivateStateFile(root, stateFileName, data, nil); err != nil {
		return ErrProjectStateUnavailable
	}
	if err := s.verifyStateRootPath(root); err != nil {
		return ErrProjectStateUnavailable
	}
	return nil
}

func (s *privateStateStore) openStateRoot(create bool) (*os.Root, error) {
	if s == nil || s.root == "" {
		return nil, ErrProjectStateUnavailable
	}
	projectInfo, err := os.Lstat(s.root)
	if err != nil || !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(projectInfo) {
		return nil, ErrProjectStateUnavailable
	}
	projectRoot, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	openedProjectInfo, err := projectRoot.Stat(".")
	if err != nil || !os.SameFile(projectInfo, openedProjectInfo) || !openedProjectInfo.IsDir() || !ownedByCurrentUser(openedProjectInfo) {
		_ = projectRoot.Close()
		return nil, ErrProjectStateUnavailable
	}
	amosRoot, err := openPinnedDirectory(projectRoot, ".amos", create, 0, nil)
	_ = projectRoot.Close()
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	stateRoot, err := openPinnedDirectory(amosRoot, "devrunner", create, 0700, nil)
	_ = amosRoot.Close()
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	return stateRoot, nil
}

// openPinnedDirectory checks the path entry before and after opening a rooted
// directory handle. The hook is used only by deterministic replacement tests.
func openPinnedDirectory(parent *os.Root, name string, create bool, privateMode os.FileMode, afterLstat func() error) (*os.Root, error) {
	if parent == nil || !safeDirectoryName(name) {
		return nil, ErrProjectStateUnavailable
	}
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if mkdirErr := parent.Mkdir(name, 0700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return nil, ErrProjectStateUnavailable
		}
		info, err = parent.Lstat(name)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(info) {
		return nil, ErrProjectStateUnavailable
	}
	if afterLstat != nil {
		if err := afterLstat(); err != nil {
			return nil, ErrProjectStateUnavailable
		}
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	openedInfo, err := child.Stat(".")
	entryInfo, entryErr := parent.Lstat(name)
	if err != nil || entryErr != nil || !entryInfo.IsDir() || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entryInfo) || !os.SameFile(info, openedInfo) || !openedInfo.IsDir() || !ownedByCurrentUser(openedInfo) {
		_ = child.Close()
		return nil, ErrProjectStateUnavailable
	}
	if privateMode != 0 && openedInfo.Mode().Perm() != privateMode {
		if err := child.Chmod(".", privateMode); err != nil {
			_ = child.Close()
			return nil, ErrProjectStateUnavailable
		}
		openedInfo, err = child.Stat(".")
		if err != nil || !os.SameFile(info, openedInfo) || openedInfo.Mode().Perm() != privateMode || !ownedByCurrentUser(openedInfo) {
			_ = child.Close()
			return nil, ErrProjectStateUnavailable
		}
	}
	return child, nil
}

func safeDirectoryName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00")
}

func readPrivateStateFile(root *os.Root, name string, afterLstat func() error) ([]byte, error) {
	if root == nil || name != stateFileName {
		return nil, ErrProjectStateUnavailable
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0600 || !ownedByCurrentUser(info) || info.Size() > maxStateBytes {
		return nil, ErrProjectStateUnavailable
	}
	if afterLstat != nil {
		if err := afterLstat(); err != nil {
			return nil, ErrProjectStateUnavailable
		}
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrProjectStateUnavailable
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	entryInfo, entryErr := root.Lstat(name)
	if err != nil || entryErr != nil || !entryInfo.Mode().IsRegular() || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entryInfo) || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0600 || !ownedByCurrentUser(openedInfo) || openedInfo.Size() > maxStateBytes {
		return nil, ErrProjectStateUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil || len(data) > maxStateBytes {
		return nil, ErrProjectStateUnavailable
	}
	return data, nil
}

func writePrivateStateFile(root *os.Root, name string, data []byte, beforeRename func() error) error {
	if root == nil || name != stateFileName || len(data) > maxStateBytes {
		return ErrProjectStateUnavailable
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return ErrProjectStateUnavailable
	}
	tempName := name + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	file, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrProjectStateUnavailable
	}
	cleanup := func() { _ = root.Remove(tempName) }
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		cleanup()
		return ErrProjectStateUnavailable
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return ErrProjectStateUnavailable
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		cleanup()
		return ErrProjectStateUnavailable
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0600 || !ownedByCurrentUser(fileInfo) {
		_ = file.Close()
		cleanup()
		return ErrProjectStateUnavailable
	}
	if err := file.Close(); err != nil {
		cleanup()
		return ErrProjectStateUnavailable
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			cleanup()
			return ErrProjectStateUnavailable
		}
	}
	if err := root.Rename(tempName, name); err != nil {
		cleanup()
		return ErrProjectStateUnavailable
	}
	directory, err := root.Open(".")
	if err != nil {
		return ErrProjectStateUnavailable
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return ErrProjectStateUnavailable
	}
	finalInfo, err := root.Lstat(name)
	if err != nil || !os.SameFile(fileInfo, finalInfo) || !finalInfo.Mode().IsRegular() || finalInfo.Mode().Perm() != 0600 || !ownedByCurrentUser(finalInfo) {
		return ErrProjectStateUnavailable
	}
	return nil
}

func (s *privateStateStore) verifyStateRootPath(pinned *os.Root) error {
	current, err := s.openStateRoot(false)
	if err != nil {
		return ErrProjectStateUnavailable
	}
	defer func() { _ = current.Close() }()
	pinnedInfo, err := pinned.Stat(".")
	if err != nil {
		return ErrProjectStateUnavailable
	}
	currentInfo, err := current.Stat(".")
	if err != nil || !os.SameFile(pinnedInfo, currentInfo) {
		return ErrProjectStateUnavailable
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func canonicalPath(path string) (string, error) {
	if path == "" {
		return "", ErrProjectStateUnavailable
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", ErrProjectStateUnavailable
	}
	return filepath.Clean(resolved), nil
}

func canonicalExecutablePath(path string) (string, error) {
	if path == "" {
		return "", ErrProcessIdentityMismatch
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", ErrProcessIdentityMismatch
	}
	return filepath.Clean(resolved), nil
}

func validProjectIdentity(identity ProjectIdentity) bool {
	return identity.ID != uuid.Nil && identity.ID.Version() == 7 && identity.ID.Variant() == uuid.RFC4122 && slugRE.MatchString(identity.Project)
}

func validResourceKind(kind ResourceKind) bool {
	return kind == ResourceDatabase || kind == ResourceDatabaseNetwork || kind == ResourceDatabaseVolume
}

func validResources(state privateProjectState) bool {
	if len(state.Resources) != 3 {
		return false
	}
	for kind, resource := range state.Resources {
		if !validResourceKind(kind) || resource.Kind != kind || resource.Name == "" || strings.ContainsAny(resource.Name, "\x00\r\n") || (resource.State != "" && resource.State != ResourceRunning && resource.State != ResourceStopped && resource.State != ResourcePresent && resource.State != ResourceMissing && resource.State != ResourceUnknown) || !validLabels(state.Identity, kind, resource.Labels) {
			return false
		}
		expectedName := map[ResourceKind]string{
			ResourceDatabase:        state.Identity.Project + "-db-*",
			ResourceDatabaseNetwork: state.Identity.Project + "-database",
			ResourceDatabaseVolume:  state.Identity.Project + "-postgres-data",
		}[kind]
		if resource.Name != expectedName {
			return false
		}
	}
	return true
}

func validLabels(identity ProjectIdentity, kind ResourceKind, labels map[string]string) bool {
	expected, err := ResourceLabels(identity, kind)
	if err != nil || len(labels) != len(expected) {
		return false
	}
	for key, value := range expected {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func validProcessIdentity(identity ProcessIdentity) bool {
	return identity.PID >= 2 && identity.StartSeconds > 0 && identity.StartMicroseconds >= 0 && identity.StartMicroseconds < 1_000_000 && filepath.IsAbs(identity.Executable) && filepath.IsAbs(identity.OwnedRoot)
}

var podmanIDRE = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

var errResourceNotFound = errors.New("local resource not found")

func inspectPodmanResources(ctx context.Context, state privateProjectState, binary string) ([]ResourceSnapshot, error) {
	if binary == "" {
		binary = "podman"
	}
	idsOutput, err := runPodmanStatus(ctx, binary, "ps", "--all", "--filter", "label="+projectLabel+"="+state.Identity.Project, "--filter", "label="+resourceLabel+"="+string(ResourceDatabase), "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	resources := make([]ResourceSnapshot, 0, 4)
	containerLabels, err := ResourceLabels(state.Identity, ResourceDatabase)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(idsOutput))
	if len(ids) == 0 {
		resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-*", State: ResourceMissing, Labels: containerLabels})
	} else {
		for _, id := range ids {
			if !podmanIDRE.MatchString(id) {
				resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-unknown", State: ResourceUnknown, Labels: containerLabels})
				continue
			}
			output, inspectErr := runPodmanStatus(ctx, binary, "inspect", "--format", "{{json .Config.Labels}}\t{{.State.Status}}\t{{.Name}}\t{{.Id}}", id)
			if inspectErr != nil {
				resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-unknown", ID: id, State: ResourceUnknown, Labels: containerLabels})
				continue
			}
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) != 4 {
				resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-unknown", ID: id, State: ResourceUnknown, Labels: containerLabels})
				continue
			}
			labels, labelsErr := decodeResourceLabels(parts[0])
			name := strings.TrimPrefix(parts[2], "/")
			fullID := strings.ToLower(parts[3])
			if labelsErr != nil || !exactLabels(containerLabels, labels) || !validDatabaseContainerName(state.Identity.Project, name) || !podmanIDRE.MatchString(fullID) || !strings.HasPrefix(fullID, strings.ToLower(id)) {
				resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: state.Identity.Project + "-db-unknown", ID: id, State: ResourceUnknown, Labels: containerLabels})
				continue
			}
			resources = append(resources, ResourceSnapshot{Kind: ResourceDatabase, Name: name, ID: fullID, State: containerState(parts[1]), Labels: containerLabels})
		}
	}
	for _, kind := range []ResourceKind{ResourceDatabaseNetwork, ResourceDatabaseVolume} {
		ref, ok := state.Resources[kind]
		labels, labelErr := ResourceLabels(state.Identity, kind)
		if !ok || labelErr != nil || !validLabels(state.Identity, kind, ref.Labels) {
			return nil, ErrProjectStateUnavailable
		}
		var args []string
		if kind == ResourceDatabaseNetwork {
			args = []string{"network", "inspect", "--format", "{{json .Labels}}", ref.Name}
		} else {
			args = []string{"volume", "inspect", "--format", "{{json .Labels}}", ref.Name}
		}
		output, inspectErr := runPodmanStatus(ctx, binary, args...)
		if errors.Is(inspectErr, errResourceNotFound) {
			resources = append(resources, ResourceSnapshot{Kind: kind, Name: ref.Name, State: ResourceMissing, Labels: labels})
			continue
		}
		if inspectErr != nil {
			resources = append(resources, ResourceSnapshot{Kind: kind, Name: ref.Name, State: ResourceUnknown, Labels: labels})
			continue
		}
		observed, decodeErr := decodeResourceLabels(string(output))
		if decodeErr != nil || !exactLabels(labels, observed) {
			resources = append(resources, ResourceSnapshot{Kind: kind, Name: ref.Name, State: ResourceUnknown, Labels: labels})
			continue
		}
		resources = append(resources, ResourceSnapshot{Kind: kind, Name: ref.Name, State: ResourcePresent, Labels: labels})
	}
	return resources, nil
}

func runPodmanStatus(ctx context.Context, binary string, args ...string) ([]byte, error) {
	if ctx == nil || binary == "" || len(args) == 0 {
		return nil, ErrProjectStateUnavailable
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, args...)
	var output boundedProbeBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(args) >= 2 && (args[0] == "network" || args[0] == "volume") {
			return nil, errResourceNotFound
		}
		return nil, ErrProjectStateUnavailable
	}
	return append([]byte(nil), output.Bytes()...), nil
}

type boundedProbeBuffer struct{ bytes.Buffer }

func (b *boundedProbeBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxProbeOutput {
		return 0, ErrProjectStateUnavailable
	}
	return b.Buffer.Write(data)
}

func decodeResourceLabels(raw string) (map[string]string, error) {
	var labels map[string]string
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&labels); err != nil || labels == nil {
		return nil, ErrProjectStateUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, ErrProjectStateUnavailable
	}
	return labels, nil
}

func exactLabels(expected, actual map[string]string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func validDatabaseContainerName(project, name string) bool {
	prefix := project + "-db-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	if len(suffix) != 12 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

func containerState(raw string) ResourceState {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running":
		return ResourceRunning
	case "created", "configured", "exited", "stopped", "paused", "unknown":
		return ResourceStopped
	default:
		return ResourceUnknown
	}
}
