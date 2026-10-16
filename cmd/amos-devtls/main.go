// Command amos-devtls serves the reference personal application through the
// explicit development-only TLS profile. It never initializes or migrates data.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ajent-social/amos/apphost"
	"github.com/ajent-social/amos/examples/reference/app/business"
	"github.com/ajent-social/amos/examples/reference/ui"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/storage"
	workspaces "github.com/ajent-social/amos/workspace/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if run(ctx, os.Args[1:], os.Stderr) != nil {
		fmt.Fprintln(os.Stderr, "amos-devtls: startup or serving unavailable")
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, output io.Writer) (result error) {
	flags := flag.NewFlagSet("amos-devtls", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("runtime-dir", "", "private runtime directory")
	if ctx == nil || flags.Parse(args) != nil || flags.NArg() != 0 || *directory == "" {
		return errConfiguration
	}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "PG") {
			return errConfiguration
		}
	}
	state, err := openRuntime(*directory)
	if err != nil {
		return errConfiguration
	}
	defer func() {
		if state.root.Close() != nil {
			result = errConfiguration
		}
		if state.pinned.Close() != nil {
			result = errConfiguration
		}
	}()
	// Root's pinned descriptor supplies the mailbox path on Linux, preserving
	// directory identity even if the selected path is renamed during startup.
	mail, err := state.mailDirectory()
	if err != nil {
		return errConfiguration
	}
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	host, err := apphost.NewDevelopmentTLS(startup, apphost.DevelopmentTLSConfig{Local: localConfig(state.config, mail), CertificatePEM: state.certificate, PrivateKeyPEM: state.key})
	cancel()
	if err != nil {
		return errConfiguration
	}
	defer func() {
		if host.Close() != nil {
			result = errConfiguration
		}
	}()
	origin, err := url.Parse(state.config.Origin)
	if err != nil {
		return errConfiguration
	}
	listener, err := net.Listen("tcp4", origin.Host)
	if err != nil {
		return errConfiguration
	}
	defer func() { _ = listener.Close() }()
	if _, err := fmt.Fprintln(output, "amos-devtls: starting development HTTPS"); err != nil {
		return errConfiguration
	}
	return host.Serve(ctx, listener)
}
func businessRoutes(db *storage.DB, sessions *session.Service) ([]apphost.Route, error) {
	todos, err := business.New(db)
	if err != nil {
		return nil, err
	}
	selected := func(r *http.Request) (string, error) {
		principal, ok := identity.PrincipalFromContext(r.Context())
		if !ok {
			return "", errors.New("authentication unavailable")
		}
		var workspace workspaces.Workspace
		err := db.WithTx(r.Context(), nil, func(tx *sql.Tx) error {
			store, err := workspaces.New(tx)
			if err != nil {
				return err
			}
			workspace, err = store.FindPersonalWorkspace(r.Context(), workspaces.Scope{InstallationID: principal.InstallationID(), ApplicationID: principal.ApplicationID()}, principal.PersonID())
			return err
		})
		return workspace.ID.String(), err
	}
	pages := sessions.Middleware(ui.NewHandler(todos, ui.Options{CSRFToken: sessions.CSRFToken, PersonalWorkspace: selected}))
	return []apphost.Route{{Method: "GET", Pattern: "/reference/ui.css", Handler: pages}, {Method: "GET", Pattern: "/reference/htmx.min.js", Handler: pages}, {Method: "GET", Pattern: "/reference/csrf.js", Handler: pages}, {Method: "GET", Pattern: "/", Handler: pages}, {Method: "GET", Pattern: "/todos", Handler: pages}, {Method: "POST", Pattern: "/todos", Handler: pages}, {Method: "GET", Pattern: "/todos/*", Handler: pages}, {Method: "POST", Pattern: "/todos/*", Handler: pages}}, nil
}
