// Package host contains private composition components. No public host,
// listener, migration installer or business mutation boundary is admitted here.
package host

import (
	"context"
	"errors"
	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	workspacecontext "github.com/ajent-social/amos/workspace/context"
	"github.com/google/uuid"
	"sync"
)

var (
	errConfiguration = errors.New("invalid continuity composition")
	errUnavailable   = errors.New("continuity unavailable")
	errDenied        = errors.New("continuity read denied")
	errNotFound      = errors.New("continuity source not found")
	errInvalid       = errors.New("invalid continuity source request")
)

type coreConfig struct {
	Database                                     storage.RuntimeConfig
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	Origin                                       string
	DevelopmentLoopback                          bool
}
type readCore struct {
	database   *storage.RuntimeDB
	root       *aw.Root
	sessions   *session.Service
	workspaces *workspacecontext.Resolver
	realm      workspacecontext.Config
	closeOnce  sync.Once
	closeErr   error
}

func validCoreConfig(cfg coreConfig) bool {
	for _, id := range []uuid.UUID{cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID} {
		if id == uuid.Nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return false
		}
	}
	_, e := deliveryemail.ParseApplicationOrigin(cfg.Origin, cfg.DevelopmentLoopback)
	return e == nil
}

// openReadCore accepts configuration only, never externally assembled authority
// pieces. The caller cannot provide a transaction, session service or callback.
func openReadCore(ctx context.Context, cfg coreConfig) (*readCore, error) {
	if ctx == nil || ctx.Err() != nil || !validCoreConfig(cfg) {
		return nil, errConfiguration
	}
	cfg.Database.RootCAPEM = append([]byte(nil), cfg.Database.RootCAPEM...)
	db, e := storage.OpenRuntime(ctx, cfg.Database)
	if e != nil {
		return nil, errUnavailable
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = db.Close()
		}
	}()
	root, e := aw.New(db)
	if e != nil {
		return nil, errUnavailable
	}
	sessions, e := session.NewWithWriter(root, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{cfg.Origin}, DevelopmentLoopback: cfg.DevelopmentLoopback, CookieSecure: !cfg.DevelopmentLoopback, PersistAssurance: true})
	if e != nil {
		return nil, errConfiguration
	}
	realm := workspacecontext.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID}
	resolver, e := workspacecontext.NewForTransactions(realm)
	if e != nil {
		return nil, errConfiguration
	}
	// This immutable profile selection is a mechanism, not evidence that every
	// selected writer/provider or a public host has been independently qualified.
	if e = aw.ActivateW1(root); e != nil {
		return nil, errUnavailable
	}
	result := &readCore{database: db, root: root, sessions: sessions, workspaces: resolver, realm: realm}
	adopted = true
	return result, nil
}
func (c *readCore) close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.database != nil {
			c.closeErr = c.database.Close()
		}
	})
	return c.closeErr
}
