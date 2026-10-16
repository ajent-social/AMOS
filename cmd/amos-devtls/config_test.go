package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func validConfig() runtimeConfig {
	config := url.URL{Scheme: "postgresql", User: url.UserPassword("fixture", "synthetic"), Host: "127.0.0.1:15432", Path: "/fixture", RawQuery: "sslmode=disable"}
	return runtimeConfig{Version: "amos-development-tls-v1", InstallationID: "0199df00-0000-7000-8000-000000000001", ApplicationID: "0199df00-0000-7000-8000-000000000002", EnvironmentID: "0199df00-0000-7000-8000-000000000003", Origin: "https://127.0.0.1:18443", DatabaseURL: config.String(), MaterialKey: strings.Repeat("ab", 32), RateKey: strings.Repeat("cd", 32)}
}
func configBytes(t *testing.T, cfg runtimeConfig) []byte {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func privateFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"runtime.json": configBytes(t, validConfig()), "certificate.pem": []byte("synthetic certificate"), "private-key.pem": []byte("synthetic key")} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "mail"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestRuntimeGrammar(t *testing.T) {
	good := configBytes(t, validConfig())
	if _, err := decodeRuntime(good); err != nil {
		t.Fatal("valid configuration rejected")
	}
	cases := map[string][]byte{"alias": []byte(strings.Replace(string(good), `"version"`, `"Version"`, 1)), "overwrite-alias": append([]byte(`{"Version":"CANARY_SECRET",`), good[1:]...), "duplicate": append([]byte(`{"version":"CANARY_SECRET",`), good[1:]...), "escaped-duplicate": append([]byte(`{"vers\u0069on":"CANARY_SECRET",`), good[1:]...), "unknown": append([]byte(`{"raw":"CANARY_SECRET",`), good[1:]...), "nested": []byte(strings.Replace(string(good), `"amos-development-tls-v1"`, `{"raw":"CANARY_SECRET"}`, 1)), "null": []byte(strings.Replace(string(good), `"amos-development-tls-v1"`, `null`, 1)), "trailing": append(append([]byte{}, good...), []byte(`{}`)...), "oversize": []byte(strings.Repeat(" ", 8193))}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRuntime(body); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*runtimeConfig){"version": func(c *runtimeConfig) { c.Version = "v2" }, "id": func(c *runtimeConfig) { c.InstallationID = "0199DF00-0000-7000-8000-000000000001" }, "key": func(c *runtimeConfig) { c.MaterialKey = strings.Repeat("AB", 32) }, "http": func(c *runtimeConfig) { c.Origin = "http://127.0.0.1:18443" }, "public": func(c *runtimeConfig) { c.Origin = "https://192.0.2.1:18443" }} {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			mutate(&cfg)
			if _, err := decodeRuntime(configBytes(t, cfg)); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
func TestDatabaseGrammar(t *testing.T) {
	for _, value := range []string{"postgresql://fixture:synthetic@127.0.0.1:15432/fixture?sslmode=disable"} {
		if !validDatabaseURL(value) {
			t.Fatal("complete local URL rejected")
		}
	}
	for _, value := range []string{"", "host=127.0.0.1", "postgresql:///fixture", "postgresql://fixture@127.0.0.1:15432/fixture?sslmode=disable", "postgresql://fixture:synthetic@localhost:15432/fixture?sslmode=disable", "postgresql://fixture:synthetic@127.0.0.1/fixture?sslmode=disable", "postgresql://fixture:synthetic@127.0.0.1:15432/?sslmode=disable", "postgresql://fixture:synthetic@127.0.0.1:15432/fixture", "postgresql://fixture:synthetic@127.0.0.1:15432/fixture?sslmode=disable&service=ambient", "postgresql://fixture:synthetic@127.0.0.1:15432/fixture?sslmode=disable&host=foreign", "postgresql://fixture:synthetic@127.0.0.1:15432/fixture?sslmode=disable&sslmode=disable"} {
		if validDatabaseURL(value) {
			t.Fatal("incomplete/ambient URL accepted")
		}
	}
}
func TestPrivateRuntime(t *testing.T) {
	dir := privateFiles(t)
	state, err := openRuntime(dir)
	if err != nil {
		t.Fatal("private files rejected")
	}
	if err := state.root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := state.pinned.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime.json", "certificate.pem", "private-key.pem", "mail", "."} {
		t.Run("mode-"+name, func(t *testing.T) {
			dir := privateFiles(t)
			path := filepath.Join(dir, name)
			if err := os.Chmod(path, 0755); err != nil {
				t.Fatal(err)
			}
			if state, err := openRuntime(dir); err == nil {
				_ = state.root.Close()
				_ = state.pinned.Close()
				t.Fatal("permissive mode accepted")
			}
		})
	}
	for _, name := range []string{"runtime.json", "certificate.pem", "private-key.pem", "mail", "root"} {
		t.Run("symlink-"+name, func(t *testing.T) {
			dir := privateFiles(t)
			if name == "root" {
				link := dir + "-link"
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(link) })
				dir = link
			} else {
				path := filepath.Join(dir, name)
				target := path + "-original"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if state, err := openRuntime(dir); err == nil {
				_ = state.root.Close()
				_ = state.pinned.Close()
				t.Fatal("symlink accepted")
			}
		})
	}
	for _, name := range []string{"runtime.json", "certificate.pem", "private-key.pem"} {
		t.Run("size-"+name, func(t *testing.T) {
			dir := privateFiles(t)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", 65537)), 0600); err != nil {
				t.Fatal(err)
			}
			if state, err := openRuntime(dir); err == nil {
				_ = state.root.Close()
				_ = state.pinned.Close()
				t.Fatal("oversize accepted")
			}
		})
	}
}

func TestPinnedDirectorySurvivesReplacement(t *testing.T) {
	dir := privateFiles(t)
	state, err := openRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.root.Close(); _ = state.pinned.Close() }()
	moved := dir + "-moved"
	if os.Rename(dir, moved) != nil {
		t.Fatal("rename failed")
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if os.Mkdir(dir, 0700) != nil || os.Mkdir(filepath.Join(dir, "mail"), 0700) != nil {
		t.Fatal("substitute creation failed")
	}
	marker := []byte("ORIGINAL_PRIVATE_CAPTURE")
	if os.WriteFile(filepath.Join(moved, "mail", "marker"), marker, 0600) != nil {
		t.Fatal("marker write failed")
	}
	mail, err := state.mailDirectory()
	if err != nil {
		t.Fatal(err)
	}
	path := mail + "/marker"
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(marker) {
		t.Fatal("pinned descriptor rerouted to substitute")
	}
	if _, err := os.Stat(filepath.Join(dir, "mail", "marker")); !os.IsNotExist(err) {
		t.Fatal("substitute received private material")
	}
}

type overriddenOwner struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (info overriddenOwner) Sys() any { return &info.stat }
func TestForeignOwnerRejected(t *testing.T) {
	dir := privateFiles(t)
	for _, name := range []string{".", "runtime.json", "mail"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatal("required owner metadata absent")
		}
		altered := *stat
		altered.Uid = uint32(os.Geteuid()) + 1
		mode := os.FileMode(0600)
		if info.IsDir() {
			mode = 0700
		}
		if owned(overriddenOwner{FileInfo: info, stat: altered}, mode, info.IsDir()) {
			t.Fatal("foreign owner accepted")
		}
	}
}
