// Package initializer validates an application request and stages generated
// files in a resumable transaction before atomically publishing its directory.
package initializer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ajent-social/amos/internal/initializer/atomic"
	"golang.org/x/mod/module"
)

const SchemaVersion = 1
const TemplateVersion = "1.1"

var (
	ErrInvalidInput              = errors.New("invalid initializer input")
	ErrGeneratorUnavailable      = errors.New("initializer generator unavailable")
	ErrTargetConflict            = errors.New("initializer target already exists or changed")
	ErrResumeConflict            = errors.New("initializer staging state is not safely resumable")
	ErrCanceled                  = errors.New("initializer canceled before finalization")
	ErrCanceledAfterFinalization = errors.New("initializer canceled after finalization")
	ErrGeneration                = errors.New("initializer generation failed")
	ErrFinalization              = errors.New("initializer finalization failed")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Input is an untrusted schema-versioned initializer request. ParentDir must
// already exist; Target is a relative directory beneath it.
type Input struct {
	SchemaVersion      int      `json:"schemaVersion"`
	AppSlug            string   `json:"appSlug"`
	ModulePath         string   `json:"module"`
	ParentDir          string   `json:"parentDir"`
	Target             string   `json:"target"`
	Modules            []string `json:"modules"`
	PublicOrigin       string   `json:"publicOrigin"`
	BusinessMode       string   `json:"businessMode"`
	Mode               string   `json:"mode"`
	DeploymentProfile  string   `json:"deploymentProfile,omitempty"`
	CloudflareRequired bool     `json:"cloudflareRequired,omitempty"`
}

// Config is immutable after validation. Its slice accessors return copies.
type Config struct {
	appSlug, modulePath, publicOrigin, businessMode, mode, deploymentProfile string
	modules                                                                  []string
}

func (c Config) AppSlug() string           { return c.appSlug }
func (c Config) ModulePath() string        { return c.modulePath }
func (c Config) PublicOrigin() string      { return c.publicOrigin }
func (c Config) BusinessMode() string      { return c.businessMode }
func (c Config) Mode() string              { return c.mode }
func (c Config) DeploymentProfile() string { return c.deploymentProfile }
func (c Config) Modules() []string         { return append([]string(nil), c.modules...) }

// Generator emits files only through Files. A nil generator is an error; the
// initializer never reports success using a placeholder implementation.
type Generator interface {
	Generate(context.Context, Config, *Files) error
}

type GeneratorFunc func(context.Context, Config, *Files) error

func (f GeneratorFunc) Generate(ctx context.Context, c Config, files *Files) error {
	return f(ctx, c, files)
}

// FileRecord describes generated output without exposing absolute paths.
type FileRecord struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	Owner  string `json:"owner"`
}

// Manifest is written into the generated project and contains only public
// project configuration and relative generated-file digests.
type ModuleRecord struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type Manifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	TemplateVersion string         `json:"templateVersion"`
	AppSlug         string         `json:"appSlug"`
	ModulePath      string         `json:"module"`
	Modules         []ModuleRecord `json:"modules"`
	Mode            string         `json:"mode"`
	BusinessMode    string         `json:"businessMode"`
	PublicOrigin    string         `json:"publicOrigin"`
	Files           []FileRecord   `json:"files"`
}

type Result struct {
	Target      string
	StagingName string
	Manifest    Manifest
	Finalized   bool
}

// DecodeInput rejects unknown fields so credential values and stale schema
// fields cannot silently pass through the initializer boundary.
func DecodeInput(r io.Reader) (Input, error) {
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Input{}, ErrInvalidInput
	}
	var input Input
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Input{}, ErrInvalidInput
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Input{}, ErrInvalidInput
	}
	return input, nil
}

// Initialize validates, stages and publishes a new generated application.
func Initialize(ctx context.Context, input Input, generator Generator) (Result, error) {
	return initialize(ctx, input, generator, transactionOps{})
}

// Resume continues an owned sibling staging directory left by Initialize. The
// caller supplies only its basename; absolute and escaping paths are rejected.
func Resume(ctx context.Context, input Input, stagingName string, generator Generator) (Result, error) {
	return resume(ctx, input, stagingName, generator, transactionOps{})
}

type transactionOps struct {
	beforeRename func(context.Context) error
	afterRename  func(context.Context) error
	rename       func(string, string) error
}

func initialize(ctx context.Context, input Input, generator Generator, ops transactionOps) (Result, error) {
	if generator == nil {
		return Result{}, ErrGeneratorUnavailable
	}
	config, target, targetParent, err := validateInput(input)
	if err != nil {
		return Result{}, err
	}
	if ctx == nil || ctx.Err() != nil {
		return Result{}, ErrCanceled
	}
	if err := targetAbsent(target); err != nil {
		return Result{}, err
	}
	parentIdentity, err := atomic.DirectoryIdentity(targetParent)
	if err != nil {
		return Result{}, ErrFinalization
	}
	stageID := randomID()
	if stageID == "" {
		return Result{}, ErrGeneration
	}
	stage, err := os.MkdirTemp(targetParent, ".amos-init-"+config.appSlug+"-")
	if err != nil {
		return Result{}, ErrGeneration
	}
	if err := os.Chmod(stage, 0700); err != nil {
		_ = os.Remove(stage)
		return Result{}, ErrGeneration
	}
	stageIdentity, identityErr := atomic.DirectoryIdentity(stage)
	if identityErr != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	j := journal{StageDigest: stageIdentity, SchemaVersion: SchemaVersion, TemplateVersion: TemplateVersion, ConfigDigest: configDigest(input, config), ParentDigest: parentIdentity, StageID: stageID, Target: filepath.ToSlash(input.Target), State: "generating", Files: map[string]FileRecord{}}
	if err := writeJournal(stage, j); err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	return runGeneration(ctx, input, config, target, targetParent, stage, j, generator, ops)
}

func resume(ctx context.Context, input Input, stagingName string, generator Generator, ops transactionOps) (Result, error) {
	if generator == nil {
		return Result{}, ErrGeneratorUnavailable
	}
	config, target, targetParent, err := validateInput(input)
	if err != nil {
		return Result{}, err
	}
	if !safeStageName(stagingName, config.appSlug) {
		return Result{}, ErrResumeConflict
	}
	stage := filepath.Join(targetParent, stagingName)
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Result{}, ErrResumeConflict
	}
	parentIdentity, identityErr := atomic.DirectoryIdentity(targetParent)
	j, err := readJournal(stage)
	stageIdentity, stageErr := atomic.DirectoryIdentity(stage)
	if stageErr != nil || j.StageDigest != stageIdentity || identityErr != nil || err != nil || j.SchemaVersion != SchemaVersion || j.TemplateVersion != TemplateVersion || j.Target != filepath.ToSlash(input.Target) || j.ConfigDigest != configDigest(input, config) || j.ParentDigest != parentIdentity || !validID(j.StageID) {
		return Result{}, ErrResumeConflict
	}
	if err := recoverJournalTemp(stage); err != nil {
		return Result{}, ErrResumeConflict
	}
	if err := recoverPending(stage, &j); err != nil {
		return Result{}, ErrResumeConflict
	}
	if err := recoverManifest(stage, &j); err != nil {
		return Result{}, ErrResumeConflict
	}
	if err := validateStageContents(stage, j); err != nil {
		return Result{}, ErrResumeConflict
	}
	if ctx == nil || ctx.Err() != nil {
		return Result{StagingName: stagingName}, ErrCanceled
	}
	if j.State == "validated" {
		if err := validateStageContents(stage, j); err != nil {
			return Result{StagingName: stagingName}, ErrResumeConflict
		}
		manifest := buildManifest(config, j)
		if err := verifyManifest(stage, manifest); err != nil {
			return Result{StagingName: stagingName}, ErrResumeConflict
		}
		return finalize(ctx, input, target, targetParent, stage, manifest, j, ops)
	}
	if j.State != "generating" {
		return Result{StagingName: stagingName}, ErrResumeConflict
	}
	return runGeneration(ctx, input, config, target, targetParent, stage, j, generator, ops)
}

func runGeneration(ctx context.Context, input Input, config Config, target, targetParent, stage string, j journal, generator Generator, ops transactionOps) (Result, error) {
	files := &Files{root: stage, journal: &j}
	if err := generator.Generate(ctx, config, files); err != nil {
		if ctx.Err() != nil {
			return Result{StagingName: filepath.Base(stage)}, ErrCanceled
		}
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	if ctx.Err() != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrCanceled
	}
	if err := validateStageContents(stage, j); err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrResumeConflict
	}
	manifest := buildManifest(config, j)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	data = append(data, '\n')
	j.State = "manifest-writing"
	j.ManifestDigest = digest(data)
	j.ManifestTemp = ".amos-manifest.tmp"
	if err := writeJournal(stage, j); err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	if err := writeManifest(stage, data); err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	j.State = "validated"
	j.ManifestDigest = ""
	j.ManifestTemp = ""
	if err := writeJournal(stage, j); err != nil {
		return Result{StagingName: filepath.Base(stage)}, ErrGeneration
	}
	return finalize(ctx, input, target, targetParent, stage, manifest, j, ops)
}

func finalize(ctx context.Context, input Input, target, targetParent, stage string, manifest Manifest, j journal, ops transactionOps) (Result, error) {
	stageName := filepath.Base(stage)
	if ctx.Err() != nil {
		return Result{StagingName: stageName, Manifest: manifest}, ErrCanceled
	}
	if ops.beforeRename != nil {
		if err := ops.beforeRename(ctx); err != nil || ctx.Err() != nil {
			return Result{StagingName: stageName, Manifest: manifest}, ErrCanceled
		}
	}
	if err := revalidateFinalization(input, target, targetParent, stage, j.ParentDigest); err != nil {
		return Result{StagingName: stageName, Manifest: manifest}, err
	}
	var committed bool
	var finalErr error
	if ops.rename != nil {
		finalErr = ops.rename(stage, target)
		committed = finalErr == nil
	} else {
		committed, finalErr = atomic.FinalizeNoReplace(stage, target, j.ParentDigest, j.StageDigest)
	}
	if !committed {
		return Result{StagingName: stageName, Manifest: manifest}, ErrFinalization
	}

	result := Result{Target: input.Target, Manifest: manifest, Finalized: true}
	_ = os.Remove(filepath.Join(target, ".amos-resume.json"))
	if dir, err := os.Open(target); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	if finalErr != nil {
		return result, ErrFinalization
	}
	if ops.afterRename != nil {
		if err := ops.afterRename(ctx); err != nil || ctx.Err() != nil {
			return result, ErrCanceledAfterFinalization
		}
	}
	if ctx.Err() != nil {
		return result, ErrCanceledAfterFinalization
	}
	return result, nil
}

// Files is a staging-only writer. It does not expose the staging directory path.
type Files struct {
	root    string
	journal *journal
}

func (f *Files) WriteFile(ctx context.Context, name string, data []byte, mode os.FileMode) error {
	if f == nil || f.journal == nil || !safeOutputPath(name) || reservedOutputPath(name) || (mode != 0600 && mode != 0644 && mode != 0755) || len(data) > 8<<20 {
		return ErrInvalidInput
	}
	if ctx == nil || ctx.Err() != nil {
		return ErrCanceled
	}
	if f.journal.Pending != nil {
		return ErrResumeConflict
	}
	if record, exists := f.journal.Files[name]; exists {
		if err := verifyFile(f.root, record); err != nil {
			return ErrResumeConflict
		}
		if record.SHA256 != digest(data) || record.Mode != uint32(mode.Perm()) {
			return ErrResumeConflict
		}
		return nil
	}
	if len(f.journal.Files) >= 128 {
		return ErrInvalidInput
	}
	total := int64(len(data))
	for _, existing := range f.journal.Files {
		total += existing.Size
	}
	if total > 32<<20 {
		return ErrInvalidInput
	}
	if err := ensureParentDirs(f.root, name); err != nil {
		return ErrGeneration
	}
	fileRecord := FileRecord{Path: name, SHA256: digest(data), Mode: uint32(mode.Perm()), Size: int64(len(data)), Owner: "managed"}
	tempName := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(name)), ".amos-write-"+f.journal.StageID+"-"+digest([]byte(name))[:12]))
	f.journal.Pending = &pendingWrite{Record: fileRecord, TempPath: tempName}
	if err := writeJournal(f.root, *f.journal); err != nil {
		return ErrGeneration
	}
	if err := writePendingFile(f.root, *f.journal.Pending, data); err != nil {
		return ErrGeneration
	}
	f.journal.Files[name] = fileRecord
	f.journal.Pending = nil
	if err := writeJournal(f.root, *f.journal); err != nil {
		return ErrGeneration
	}
	return nil
}

func writePendingFile(root string, pending pendingWrite, data []byte) error {
	if !safeOutputPath(pending.TempPath) || filepath.Dir(pending.TempPath) != filepath.Dir(pending.Record.Path) {
		return ErrInvalidInput
	}
	if err := ensureParentDirs(root, pending.Record.Path); err != nil {
		return err
	}
	temp := filepath.Join(root, filepath.FromSlash(pending.TempPath))
	dst := filepath.Join(root, filepath.FromSlash(pending.Record.Path))
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(os.FileMode(pending.Record.Mode))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(temp, dst); err != nil {
		return err
	}
	if err := os.Remove(temp); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(dst))
}

func recoverPending(root string, j *journal) error {
	if j.Pending == nil {
		return nil
	}
	pending := *j.Pending
	expectedTemp := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(pending.Record.Path)), ".amos-write-"+j.StageID+"-"+digest([]byte(pending.Record.Path))[:12]))
	if !safeOutputPath(pending.Record.Path) || reservedOutputPath(pending.Record.Path) || pending.TempPath != expectedTemp {
		return ErrResumeConflict
	}
	dst := filepath.Join(root, filepath.FromSlash(pending.Record.Path))
	if err := rejectSymlinkPath(root, filepath.Dir(dst)); err != nil {
		return err
	}
	info, err := os.Lstat(dst)
	if err == nil {
		if !info.Mode().IsRegular() || verifyFile(root, pending.Record) != nil {
			return ErrResumeConflict
		}
		j.Files[pending.Record.Path] = pending.Record
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrResumeConflict
	}
	temp := filepath.Join(root, filepath.FromSlash(pending.TempPath))
	if info, err := os.Lstat(temp); err == nil {
		if !info.Mode().IsRegular() {
			return ErrResumeConflict
		}
		if err := os.Remove(temp); err != nil {
			return ErrResumeConflict
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrResumeConflict
	}
	if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
		cleanupPendingDirs(root, pending.Record.Path, j.Files)
	}
	j.Pending = nil
	if err := writeJournal(root, *j); err != nil {
		return ErrResumeConflict
	}
	return nil
}

func cleanupPendingDirs(root, output string, existing map[string]FileRecord) {
	for dir := filepath.Dir(filepath.FromSlash(output)); dir != "."; dir = filepath.Dir(dir) {
		prefix := filepath.ToSlash(dir) + "/"
		used := false
		for name := range existing {
			if strings.HasPrefix(name, prefix) {
				used = true
				break
			}
		}
		if used {
			continue
		}
		full := filepath.Join(root, dir)
		info, err := os.Lstat(full)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		_ = os.Remove(full) // removes only an empty directory; authored content remains untouched
	}
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func validateInput(input Input) (Config, string, string, error) {
	invalid := func() (Config, string, string, error) { return Config{}, "", "", ErrInvalidInput }
	if input.SchemaVersion != SchemaVersion || !slugPattern.MatchString(input.AppSlug) || module.CheckPath(input.ModulePath) != nil || input.ParentDir == "" || input.Target == "" {
		return invalid()
	}
	if !safeTarget(input.Target) || (input.BusinessMode != "integrated-go" && input.BusinessMode != "private-service") {
		return invalid()
	}
	config := Config{appSlug: input.AppSlug, modulePath: input.ModulePath, publicOrigin: input.PublicOrigin, businessMode: input.BusinessMode, mode: input.Mode, deploymentProfile: input.DeploymentProfile}
	if err := validateModules(input.Modules, &config); err != nil {
		return invalid()
	}
	if err := validateMode(input, &config); err != nil {
		return invalid()
	}
	moduleHost := strings.SplitN(input.ModulePath, "/", 2)[0]
	u, _ := url.Parse(input.PublicOrigin)
	if u == nil || strings.EqualFold(moduleHost, u.Hostname()) {
		return invalid()
	}
	parentAbs, err := filepath.Abs(input.ParentDir)
	if err != nil {
		return invalid()
	}
	parentReal, err := filepath.EvalSymlinks(parentAbs)
	if err != nil {
		return Config{}, "", "", ErrInvalidInput
	}
	info, err := os.Stat(parentReal)
	if err != nil || !info.IsDir() {
		return invalid()
	}
	target := filepath.Join(parentReal, filepath.FromSlash(input.Target))
	targetParent := filepath.Dir(target)
	if err := atomic.SecureParent(targetParent); err != nil {
		return invalid()
	}
	if err := ensureSafeExistingPath(parentReal, targetParent); err != nil {
		return Config{}, "", "", ErrInvalidInput
	}
	return config, target, targetParent, nil
}

type moduleDefinition struct {
	version      string
	dependencies []string
}

var supportedModules = map[string]moduleDefinition{
	"identity":  {version: "1.0"},
	"workspace": {version: "1.0", dependencies: []string{"identity"}},
	"billing":   {version: "1.0"},
}

func validateModules(input []string, config *Config) error {
	if len(input) == 0 || len(input) > 32 {
		return ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, name := range input {
		if _, known := supportedModules[name]; !known || seen[name] {
			return ErrInvalidInput
		}
		seen[name] = true
		config.modules = append(config.modules, name)
	}
	for name := range seen {
		for _, required := range supportedModules[name].dependencies {
			if !seen[required] {
				return ErrInvalidInput
			}
		}
	}
	sort.Strings(config.modules)
	return nil
}

func validateMode(input Input, config *Config) error {
	u, err := url.Parse(input.PublicOrigin)
	if err != nil || u.Opaque != "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return ErrInvalidInput
	}
	switch input.Mode {
	case "evaluation":
		host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
		if input.DeploymentProfile != "" || input.CloudflareRequired || u.Scheme != "http" || (host != "localhost" && net.ParseIP(host) == nil) || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
			return ErrInvalidInput
		}
	case "production-target":
		if u.Scheme != "https" || (input.DeploymentProfile != "managed" && input.DeploymentProfile != "small-vm") || !input.CloudflareRequired || net.ParseIP(u.Hostname()) != nil || module.CheckPath(u.Hostname()+"/origin") != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	config.publicOrigin = input.PublicOrigin
	return nil
}

func safeTarget(target string) bool {
	if target == "" || filepath.IsAbs(target) || filepath.VolumeName(target) != "" || strings.ContainsAny(target, "\\\x00") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(target))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return filepath.ToSlash(clean) == target
}
func safeStageName(name, slug string) bool {
	return filepath.Base(name) == name && strings.HasPrefix(name, ".amos-init-"+slug+"-") && !strings.ContainsAny(name, "/\\")
}
func safeOutputPath(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func reservedOutputPath(name string) bool {
	base := filepath.Base(name)
	return name == ".amos-manifest.json" || name == ".amos-manifest.tmp" || name == ".amos-resume.json" || strings.HasPrefix(base, ".amos-resume.json.") || strings.HasPrefix(base, ".amos-write-")
}
func ensureSafeExistingPath(root, targetParent string) error {
	rel, err := filepath.Rel(root, targetParent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrInvalidInput
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidInput
		}
	}
	return nil
}
func ensureParentDirs(root, name string) error {
	parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(name)))
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrInvalidInput
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0755); err != nil {
				return err
			}
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidInput
		}
	}
	return nil
}
func targetAbsent(target string) error {
	_, err := os.Lstat(target)
	if err == nil {
		return ErrTargetConflict
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ErrTargetConflict
	}
	return nil
}
func revalidateFinalization(input Input, target, targetParent, stage, parentDigest string) error {
	parentAbs, err := filepath.Abs(input.ParentDir)
	if err != nil {
		return ErrTargetConflict
	}
	parentReal, err := filepath.EvalSymlinks(parentAbs)
	if err != nil {
		return ErrTargetConflict
	}
	if err := ensureSafeExistingPath(parentReal, targetParent); err != nil {
		return ErrTargetConflict
	}
	identity, err := atomic.DirectoryIdentity(targetParent)
	if err != nil || identity != parentDigest {
		return ErrTargetConflict
	}
	if err := targetAbsent(target); err != nil {
		return err
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil || !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
		return ErrTargetConflict
	}
	return nil
}

func moduleRecords(c Config) []ModuleRecord {
	modules := make([]ModuleRecord, 0, len(c.modules))
	for _, id := range c.modules {
		modules = append(modules, ModuleRecord{ID: id, Version: supportedModules[id].version})
	}
	return modules
}

func buildManifest(c Config, j journal) Manifest {
	modules := moduleRecords(c)
	files := make([]FileRecord, 0, len(j.Files))
	for _, entry := range j.Files {
		files = append(files, entry)
	}
	sort.Slice(files, func(i, k int) bool { return files[i].Path < files[k].Path })
	return Manifest{SchemaVersion: SchemaVersion, TemplateVersion: TemplateVersion, AppSlug: c.appSlug, ModulePath: c.modulePath, Modules: modules, Mode: c.mode, BusinessMode: c.businessMode, PublicOrigin: c.publicOrigin, Files: files}
}
func configDigest(input Input, c Config) string {
	data, _ := json.Marshal(struct {
		Version                                                       int
		Template                                                      string
		Slug, ModulePath, Origin, BusinessMode, Mode, Profile, Target string
		Modules                                                       []ModuleRecord
	}{SchemaVersion, TemplateVersion, c.appSlug, c.modulePath, c.publicOrigin, c.businessMode, c.mode, c.deploymentProfile, filepath.ToSlash(input.Target), moduleRecords(c)})
	return digest(data)
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func validID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func writeManifest(root string, data []byte) error {
	temp := filepath.Join(root, ".amos-manifest.tmp")
	destination := filepath.Join(root, ".amos-manifest.json")
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(temp, destination); err != nil {
		return err
	}
	if err := os.Remove(temp); err != nil {
		return err
	}
	return syncDirectory(root)
}

func recoverManifest(root string, j *journal) error {
	if j.State != "manifest-writing" {
		return nil
	}
	if j.ManifestTemp != ".amos-manifest.tmp" || len(j.ManifestDigest) != 64 {
		return ErrResumeConflict
	}
	temp := filepath.Join(root, j.ManifestTemp)
	destination := filepath.Join(root, ".amos-manifest.json")
	info, err := os.Lstat(destination)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
			return ErrResumeConflict
		}
		data, readErr := os.ReadFile(destination)
		if readErr != nil || digest(data) != j.ManifestDigest {
			return ErrResumeConflict
		}
		if tempInfo, tempErr := os.Lstat(temp); tempErr == nil {
			if !tempInfo.Mode().IsRegular() {
				return ErrResumeConflict
			}
			if err := os.Remove(temp); err != nil {
				return ErrResumeConflict
			}
		} else if !errors.Is(tempErr, os.ErrNotExist) {
			return ErrResumeConflict
		}
		j.State = "validated"
	} else if errors.Is(err, os.ErrNotExist) {
		if tempInfo, tempErr := os.Lstat(temp); tempErr == nil {
			if !tempInfo.Mode().IsRegular() {
				return ErrResumeConflict
			}
			if err := os.Remove(temp); err != nil {
				return ErrResumeConflict
			}
		} else if !errors.Is(tempErr, os.ErrNotExist) {
			return ErrResumeConflict
		}
		j.State = "generating"
	} else {
		return ErrResumeConflict
	}
	j.ManifestTemp = ""
	j.ManifestDigest = ""
	return writeJournal(root, *j)
}

func writeJournal(root string, j journal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tempPath := filepath.Join(root, ".amos-resume.json.tmp")
	tmp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tempPath, filepath.Join(root, ".amos-resume.json")); err != nil {
		return err
	}
	return syncDirectory(root)
}

func recoverJournalTemp(root string) error {
	path := filepath.Join(root, ".amos-resume.json.tmp")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return ErrResumeConflict
	}
	return os.Remove(path)
}

type pendingWrite struct {
	Record   FileRecord `json:"record"`
	TempPath string     `json:"tempPath"`
}

type journal struct {
	SchemaVersion   int                   `json:"schemaVersion"`
	TemplateVersion string                `json:"templateVersion"`
	ConfigDigest    string                `json:"configDigest"`
	StageDigest     string                `json:"stageDigest"`
	ParentDigest    string                `json:"parentDigest"`
	StageID         string                `json:"stageId"`
	Target          string                `json:"target"`
	State           string                `json:"state"`
	Files           map[string]FileRecord `json:"files"`
	Pending         *pendingWrite         `json:"pending,omitempty"`
	ManifestTemp    string                `json:"manifestTemp,omitempty"`
	ManifestDigest  string                `json:"manifestDigest,omitempty"`
}

func verifyManifest(root string, want Manifest) error {
	data, err := os.ReadFile(filepath.Join(root, ".amos-manifest.json"))
	if err != nil {
		return ErrResumeConflict
	}
	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		return ErrResumeConflict
	}
	expected, _ := json.MarshalIndent(want, "", "  ")
	expected = append(expected, '\n')
	if !bytes.Equal(data, expected) {
		return ErrResumeConflict
	}
	return nil
}

func readJournal(root string) (journal, error) {
	journalPath := filepath.Join(root, ".amos-resume.json")
	info, err := os.Lstat(journalPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return journal{}, ErrResumeConflict
	}
	f, err := os.Open(journalPath)
	if err != nil {
		return journal{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	closeErr := f.Close()
	if err != nil {
		return journal{}, err
	}
	if closeErr != nil {
		return journal{}, closeErr
	}
	var j journal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&j); err != nil {
		return journal{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return journal{}, ErrResumeConflict
	}
	if j.Files == nil {
		return journal{}, errors.New("missing files")
	}
	return j, nil
}
func verifyFile(root string, record FileRecord) error {
	if !safeOutputPath(record.Path) || reservedOutputPath(record.Path) || record.Owner != "managed" {
		return ErrResumeConflict
	}
	full := filepath.Join(root, filepath.FromSlash(record.Path))
	if err := rejectSymlinkPath(root, full); err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != record.Mode || info.Size() != record.Size {
		return ErrResumeConflict
	}
	data, err := os.ReadFile(full)
	if err != nil || digest(data) != record.SHA256 {
		return ErrResumeConflict
	}
	return nil
}
func rejectSymlinkPath(root, full string) error {
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrResumeConflict
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrResumeConflict
		}
	}
	return nil
}
func validateStageContents(root string, j journal) error {
	expectedDirs := map[string]bool{".": true}
	addDirs := func(name string) {
		for dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(name))); dir != "."; dir = filepath.ToSlash(filepath.Dir(filepath.FromSlash(dir))) {
			expectedDirs[dir] = true
		}
	}
	for name := range j.Files {
		addDirs(name)
	}
	if j.Pending != nil {
		addDirs(j.Pending.Record.Path)
		addDirs(j.Pending.TempPath)
	}
	for name, record := range j.Files {
		if name != record.Path || verifyFile(root, record) != nil {
			return ErrResumeConflict
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrResumeConflict
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if !expectedDirs[rel] {
				return ErrResumeConflict
			}
			return nil
		}
		if rel == ".amos-resume.json" {
			return nil
		}
		if rel == ".amos-manifest.tmp" && j.State == "manifest-writing" {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return ErrResumeConflict
			}
			return nil
		}
		if rel == ".amos-manifest.json" && (j.State == "validated" || j.State == "manifest-writing") {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
				return ErrResumeConflict
			}
			return nil
		}
		if _, ok := j.Files[rel]; !ok {
			return ErrResumeConflict
		}
		return nil
	})
}
