// Package local generates the Podman assets used by a developer's local
// environment. It does not start containers or qualify a production database.
package local

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"embed"

	"github.com/ajent-social/amos/internal/initializer"
)

//go:embed templates/*
var templates embed.FS

const (
	PostgresImage = "docker.io/library/postgres:16.15-bookworm@sha256:efedf3595f1d6f415c08568ba171029bf54052e754cc9f030e3f2412b21f3d67"
	DefaultPort   = 55432
)

var ErrPortInUse = errors.New("local database port is already in use")

// Generator implements initializer.Generator for local database setup. Private
// secrets are freshly generated for each app and written only to .env.local
// with owner-only permissions.
type Generator struct{}

type values struct {
	App         string
	Image       string
	Database    string
	Migration   string
	Runtime     string
	MigrationPW string
	RuntimePW   string
}

func (Generator) Generate(ctx context.Context, config initializer.Config, files *initializer.Files) error {
	if ctx == nil || files == nil || config.AppSlug() == "" {
		return initializer.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	v, err := makeValues(config.AppSlug())
	if err != nil {
		return err
	}
	for _, asset := range []struct {
		template string
		path     string
		mode     os.FileMode
	}{
		{template: "compose.yaml", path: "compose.yaml", mode: 0644},
		{template: "scripts/migrate", path: "scripts/migrate", mode: 0755},
		{template: "scripts/dev", path: "scripts/dev", mode: 0755},
		{template: "env.local.example", path: ".env.local.example", mode: 0644},
		{template: ".gitignore", path: ".gitignore", mode: 0644},
		{template: "db-init-roles.sh", path: "db-init-roles.sh", mode: 0644},
	} {
		data, err := render(asset.template, v)
		if err != nil {
			return err
		}
		if err := files.WriteFile(ctx, asset.path, data, asset.mode); err != nil {
			return err
		}
	}
	privateEnv := strings.Join([]string{
		fmt.Sprintf("AMOS_DB_PORT=%d", DefaultPort),
		"AMOS_DB_NAME=" + v.Database,
		"AMOS_DB_MIGRATION_USER=" + v.Migration,
		"AMOS_DB_MIGRATION_PASSWORD=" + v.MigrationPW,
		"AMOS_DB_RUNTIME_USER=" + v.Runtime,
		"AMOS_DB_RUNTIME_PASSWORD=" + v.RuntimePW,
	}, "\n") + "\n"
	return files.WriteFile(ctx, ".env.local", []byte(privateEnv), 0600)
}

func makeValues(app string) (values, error) {
	if len(app) < 1 || len(app) > 63 {
		return values{}, initializer.ErrInvalidInput
	}
	for _, r := range app {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return values{}, initializer.ErrInvalidInput
		}
	}
	migration, err := secret()
	if err != nil {
		return values{}, err
	}
	runtime, err := secret()
	if err != nil {
		return values{}, err
	}
	return values{App: app, Image: PostgresImage, Database: strings.ReplaceAll(app, "-", "_"), Migration: "amos_migrator", Runtime: "amos_runtime", MigrationPW: migration, RuntimePW: runtime}, nil
}

func secret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate local database material (%w)", err)
	}
	h := sha256.Sum256(b[:])
	return hex.EncodeToString(h[:]), nil
}

func render(name string, v values) ([]byte, error) {
	raw, err := templates.ReadFile(filepath.ToSlash(filepath.Join("templates", name+".tmpl")))
	if err != nil {
		return nil, err
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	if err := t.Execute(&out, v); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// CheckPortAvailable reserves no socket; it verifies the IPv4 loopback bind
// needed by the generated compose mapping. A later start can still race, so
// Podman remains the final authority.
func CheckPortAvailable(port string) error {
	if strings.TrimSpace(port) == "" || strings.ContainsAny(port, "\r\n") {
		return ErrPortInUse
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return ErrPortInUse
	}
	return listener.Close()
}
