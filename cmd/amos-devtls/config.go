package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/ajent-social/amos/apphost"
	delivery "github.com/ajent-social/amos/delivery/email"
	"github.com/google/uuid"
)

var errConfiguration = errors.New("development TLS configuration unavailable")

type runtimeConfig struct {
	Version        string `json:"version"`
	InstallationID string `json:"installation_id"`
	ApplicationID  string `json:"application_id"`
	EnvironmentID  string `json:"environment_id"`
	Origin         string `json:"origin"`
	DatabaseURL    string `json:"database_url"`
	MaterialKey    string `json:"material_key"`
	RateKey        string `json:"rate_key"`
}

type privateRuntime struct {
	root             *os.Root
	pinned           *os.File
	config           runtimeConfig
	certificate, key []byte
}

func owned(info os.FileInfo, mode os.FileMode, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm() == mode && info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && info.IsDir() == directory && (directory || info.Mode().IsRegular())
}
func openRuntime(directory string) (_ *privateRuntime, result error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errConfiguration
	}
	for path := directory; path != "/"; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errConfiguration
		}
	}
	before, err := os.Lstat(directory)
	if err != nil || !owned(before, 0700, true) {
		return nil, errConfiguration
	}
	fd, err := syscall.Open(directory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errConfiguration
	}
	pinned := os.NewFile(uintptr(fd), "runtime")
	defer func() {
		if result != nil {
			_ = pinned.Close()
		}
	}()
	checked, err := pinned.Stat()
	if err != nil || !os.SameFile(before, checked) {
		return nil, errConfiguration
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, errConfiguration
	}
	defer func() {
		if result != nil {
			_ = root.Close()
		}
	}()
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(checked, after) || !owned(after, 0700, true) {
		return nil, errConfiguration
	}
	body, err := readPrivate(root, "runtime.json", 8192)
	if err != nil {
		return nil, errConfiguration
	}
	cfg, err := decodeRuntime(body)
	if err != nil {
		return nil, errConfiguration
	}
	cert, err := readPrivate(root, "certificate.pem", 64<<10)
	if err != nil {
		return nil, errConfiguration
	}
	key, err := readPrivate(root, "private-key.pem", 64<<10)
	if err != nil {
		return nil, errConfiguration
	}
	mail, err := root.Lstat("mail")
	if err != nil || !owned(mail, 0700, true) {
		return nil, errConfiguration
	}
	mailFile, err := root.Open("mail")
	if err != nil {
		return nil, errConfiguration
	}
	mailAfter, statErr := mailFile.Stat()
	closeErr := mailFile.Close()
	if statErr != nil || closeErr != nil || !os.SameFile(mail, mailAfter) || !owned(mailAfter, 0700, true) {
		return nil, errConfiguration
	}
	return &privateRuntime{root: root, pinned: pinned, config: cfg, certificate: cert, key: key}, nil
}
func readPrivate(root *os.Root, name string, limit int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil || !owned(before, 0600, false) || before.Size() > limit {
		return nil, errConfiguration
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, errConfiguration
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil || !owned(after, 0600, false) || !os.SameFile(before, after) {
		return nil, errConfiguration
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errConfiguration
	}
	return body, nil
}
func decodeRuntime(body []byte) (runtimeConfig, error) {
	var cfg runtimeConfig
	if len(body) > 8192 || !utf8.Valid(body) {
		return cfg, errConfiguration
	}
	allowed := map[string]bool{"version": true, "installation_id": true, "application_id": true, "environment_id": true, "origin": true, "database_url": true, "material_key": true, "rate_key": true}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return cfg, errConfiguration
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err = decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return cfg, errConfiguration
		}
		seen[name] = true
		var value string
		if decoder.Decode(&value) != nil {
			return cfg, errConfiguration
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || len(seen) != len(allowed) {
		return cfg, errConfiguration
	}
	if json.Unmarshal(body, &cfg) != nil || cfg.Version != "amos-development-tls-v1" {
		return cfg, errConfiguration
	}
	for _, value := range []string{cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID} {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return cfg, errConfiguration
		}
	}
	for _, value := range []string{cfg.MaterialKey, cfg.RateKey} {
		key, err := hex.DecodeString(value)
		if err != nil || len(key) != 32 || hex.EncodeToString(key) != value {
			return cfg, errConfiguration
		}
	}
	if _, err := delivery.ParseDevelopmentTLSOrigin(cfg.Origin); err != nil {
		return cfg, errConfiguration
	}
	if !validDatabaseURL(cfg.DatabaseURL) {
		return cfg, errConfiguration
	}
	return cfg, nil
}
func validDatabaseURL(raw string) bool {
	if strings.ContainsAny(raw, "\r\n\t ") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.Opaque != "" || u.Fragment != "" {
		return false
	}
	phrase, ok := u.User.Password()
	if !ok || phrase == "" || u.User.Username() == "" {
		return false
	}
	if u.Hostname() != "127.0.0.1" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != u.Port() {
		return false
	}
	if u.Path == "" || u.Path == "/" || strings.Contains(u.Path[1:], "/") || u.RawPath != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["sslmode"]) != 1 || q.Get("sslmode") != "disable" {
		return false
	}
	return u.Host == net.JoinHostPort("127.0.0.1", u.Port())
}
func localConfig(config runtimeConfig, mail string) apphost.LocalConfig {
	material, _ := hex.DecodeString(config.MaterialKey)
	rate, _ := hex.DecodeString(config.RateKey)
	return apphost.LocalConfig{InstallationID: uuid.MustParse(config.InstallationID), ApplicationID: uuid.MustParse(config.ApplicationID), EnvironmentID: uuid.MustParse(config.EnvironmentID), Origin: config.Origin, DatabaseURL: config.DatabaseURL, MailDirectory: mail, MaterialKey: material, RateKey: rate, Business: businessRoutes}
}

func (state *privateRuntime) mailDirectory() (string, error) {
	if runtime.GOOS != "linux" || state == nil || state.pinned == nil {
		return "", errConfiguration
	}
	path := "/proc/self/fd/" + strconv.FormatUint(uint64(state.pinned.Fd()), 10) + "/mail"
	expected, err := state.root.Lstat("mail")
	if err != nil || !owned(expected, 0700, true) {
		return "", errConfiguration
	}
	actual, err := os.Stat(path)
	if err != nil || !owned(actual, 0700, true) || !os.SameFile(expected, actual) {
		return "", errConfiguration
	}
	return path, nil
}
