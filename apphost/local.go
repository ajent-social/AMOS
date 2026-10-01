// Package apphost composes the shared runtime for generated applications.
package apphost

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/ajent-social/amos/app"
	delivery "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/localcapture"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/identity/login"
	"github.com/ajent-social/amos/identity/mfa"
	mfavault "github.com/ajent-social/amos/identity/mfa/vault"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/password/policy"
	"github.com/ajent-social/amos/identity/primaryproof"
	"github.com/ajent-social/amos/identity/protection"
	"github.com/ajent-social/amos/identity/recovery"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/storage"
	"github.com/ajent-social/amos/ui/authpassword"
	"github.com/google/uuid"
	"mime"
)

var ErrLocalConfiguration = errors.New("local application configuration unavailable")
var ErrLocalDependency = errors.New("local application dependency unavailable")

// Route supplies integrated Go business code on the shared public domain.
type Route struct {
	Method, Pattern string
	Handler         http.Handler
}

// LocalConfig is explicitly for an unqualified loopback development environment.
// Key bytes and the database URL come from private owner-only local configuration.
type LocalConfig struct {
	InstallationID, ApplicationID, EnvironmentID uuid.UUID
	Origin, DatabaseURL, MailDirectory           string
	MaterialKey, RateKey                         []byte
	Business                                     func(*storage.DB, *session.Service) ([]Route, error)
}
type Host struct {
	app       *app.App
	db        *storage.DB
	pool      *sql.DB
	mailbox   *localcapture.Capture
	worker    jobs.Worker
	origin    *url.URL
	closeOnce sync.Once
	closeErr  error
}

// NewLocal opens already migrated dependencies. Missing migrations, private
// mailbox or keys fail visibly; this never substitutes fixtures for delivery.
func NewLocal(ctx context.Context, cfg LocalConfig) (host *Host, result error) {
	origin, err := delivery.ParseApplicationOrigin(cfg.Origin, true)
	if ctx == nil || err != nil || origin.Scheme != "http" || cfg.Business == nil || len(cfg.MaterialKey) != 32 || len(cfg.RateKey) != 32 || !localID(cfg.InstallationID) || !localID(cfg.ApplicationID) || !localID(cfg.EnvironmentID) {
		return nil, ErrLocalConfiguration
	}
	db, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, ErrLocalDependency
	}
	h := &Host{db: db, origin: origin}
	defer func() {
		if result != nil {
			result = errors.Join(result, h.Close())
		}
	}()
	if err := h.schemaReady(ctx); err != nil {
		return nil, ErrLocalDependency
	}
	if err := h.bind(ctx, cfg); err != nil {
		return nil, err
	}
	pool, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, ErrLocalDependency
	}
	h.pool = pool
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(2)
	if err := pool.PingContext(ctx); err != nil {
		return nil, ErrLocalDependency
	}
	outbox, err := sqlstore.New(pool, sqlstore.Config{ClaimKinds: []string{delivery.Kind}, MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 5, MaxLease: time.Minute, MaxRetryDelay: time.Hour})
	if err != nil {
		return nil, err
	}
	materials, err := materialstore.New(materialstore.Config{DevelopmentLoopback: true, DB: db, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ActiveKeyID: "local-v1", Keys: map[string][]byte{"local-v1": cfg.MaterialKey}, ApplicationOrigin: cfg.Origin})
	if err != nil {
		return nil, err
	}
	renderer, err := delivery.NewRenderer(delivery.RenderConfig{DevelopmentLoopback: true, FromAddress: "amos@example.invalid", ApplicationOrigin: cfg.Origin, MaxBodyBytes: delivery.MaxBodyBytes})
	if err != nil {
		return nil, err
	}
	mailbox, err := localcapture.New(cfg.MailDirectory, cfg.Origin, true)
	if err != nil {
		return nil, err
	}
	h.mailbox = mailbox
	dispatcher, err := delivery.NewDispatcher(renderer, mailbox, materials, time.Second)
	if err != nil {
		return nil, err
	}
	workerID, err := uuid.NewV7()
	if err != nil {
		return nil, ErrLocalDependency
	}
	h.worker = jobs.Worker{Repository: outbox, Consumer: dispatcher, Owner: "local-email-" + workerID.String(), Lease: time.Minute}
	sessions, err := session.New(db, session.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, AllowedOrigins: []string{cfg.Origin}, DevelopmentLoopback: true, PersistAssurance: true})
	if err != nil {
		return nil, err
	}
	limiter, err := protection.New(protection.Config{DB: db, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, Key: cfg.RateKey, Window: time.Minute, IPLimit: 40, AccountLimit: 10})
	if err != nil {
		return nil, err
	}
	blocklist, err := policy.New(nil, "")
	if err != nil {
		return nil, err
	}
	hasher, err := password.New(blocklist, limiter.PasswordBudget(), 2)
	if err != nil {
		return nil, err
	}
	verification, err := identityemail.New(db, outbox, renderer, materials, identityemail.Config{InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, ApplicationOrigin: cfg.Origin, DevelopmentLoopback: true, ChallengeLifetime: identityemail.DefaultChallengeLifetime})
	if err != nil {
		return nil, err
	}
	credentials, err := login.New(login.Config{DB: db, Passwords: hasher, Sessions: sessions, Email: verification, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID})
	if err != nil {
		return nil, err
	}
	guard := protection.Guard{Limiter: limiter, Sessions: sessions}
	signup, err := guard.PublicJSON(protection.Signup, credentials.Handler())
	if err != nil {
		return nil, err
	}
	signin, err := guard.PublicJSON(protection.Signin, credentials.Handler())
	if err != nil {
		return nil, err
	}
	recoveries, err := recovery.New(recovery.Config{DB: db, Passwords: hasher, Outbox: outbox, Renderer: renderer, Materials: materials, Policy: localPasswordPolicy{cfg.InstallationID, cfg.ApplicationID}, InstallationID: cfg.InstallationID, ApplicationID: cfg.ApplicationID, EnvironmentID: cfg.EnvironmentID, ApplicationOrigin: cfg.Origin, DevelopmentLoopback: true, ChallengeLifetime: recovery.DefaultChallengeLifetime})
	if err != nil {
		return nil, err
	}
	recoveryRequest, err := guard.PublicJSON(protection.Recovery, recoveries.RequestHandler())
	if err != nil {
		return nil, err
	}
	recoveryComplete, err := guard.PublicJSON(protection.Recovery, recoveries.CompleteHandler())
	if err != nil {
		return nil, err
	}
	verificationComplete, err := guard.PublicJSON(protection.Verification, verification.ConfirmHandler())
	if err != nil {
		return nil, err
	}
	forms, err := authpassword.New(authpassword.Config{Signup: signup, Signin: signin, RecoveryRequest: recoveryRequest, RecoveryComplete: recoveryComplete, Verification: verification, VerificationConfirm: verificationComplete, Reset: recoveries, AllowsOrigin: sessions.AllowsOrigin})
	if err != nil {
		return nil, err
	}
	pages := forms.Handler()
	compatible := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method == http.MethodPost && media == "application/json" {
			switch r.URL.Path {
			case "/signup":
				signup.ServeHTTP(w, r)
				return
			case "/auth":
				signin.ServeHTTP(w, r)
				return
			case "/forgot-password":
				recoveryRequest.ServeHTTP(w, r)
				return
			case "/reset-password":
				recoveryComplete.ServeHTTP(w, r)
				return
			case "/verify-email":
				verificationComplete.ServeHTTP(w, r)
				return
			}
		}
		pages.ServeHTTP(w, r)
	})
	primary, err := primaryproof.New(hasher)
	if err != nil {
		return nil, err
	}
	derived := hmac.New(sha256.New, cfg.MaterialKey)
	if _, err := derived.Write([]byte("AMOS local TOTP seed key v1")); err != nil {
		return nil, err
	}
	vault, err := mfavault.NewKeyring("local-v1", map[string][]byte{"local-v1": derived.Sum(nil)})
	if err != nil {
		return nil, err
	}
	factors, err := mfa.New(mfa.Config{DB: db, Vault: vault, Primary: primary, Policy: localMFAPolicy{localPasswordPolicy{cfg.InstallationID, cfg.ApplicationID}}, Sessions: sessions, Issuer: "AMOS local evaluation"})
	if err != nil {
		return nil, err
	}
	mutations, err := guard.CookieMutation(protection.MFA, factors.Handler())
	if err != nil {
		return nil, err
	}
	factorRead := sessions.Middleware(factors.Handler())
	factorWrite := sessions.Middleware(mutations)
	shared, err := app.New(app.Options{Identity: app.IdentityHandlers{TOTPStatus: factorRead, TOTPEnroll: factorWrite, TOTPConfirm: factorWrite, TOTPChallenge: factorWrite, SignupPage: compatible, SigninPage: compatible, Signup: compatible, Signin: compatible, VerifyEmail: verificationAdmission(limiter, compatible), ForgotPassword: compatible, ResetPassword: compatible, Signout: sessions.Middleware(http.HandlerFunc(sessions.SignOut))}, ReadinessChecks: []app.ReadinessCheck{func(ctx context.Context) error {
		if err := pool.PingContext(ctx); err != nil {
			return err
		}
		return h.schemaReady(ctx)
	}}})
	if err != nil {
		return nil, err
	}
	h.app = shared
	routes, err := cfg.Business(db, sessions)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		if route.Handler == nil {
			return nil, ErrLocalConfiguration
		}
		if err := shared.RegisterBusinessRoute(route.Method, route.Pattern, route.Handler); err != nil {
			return nil, err
		}
	}
	return h, nil
}
func localID(id uuid.UUID) bool { return id.Version() == 7 && id.Variant() == uuid.RFC4122 }

// Serve verifies the actual listener is the configured loopback origin before
// serving insecure development cookies, and owns the mail worker's lifetime.
func (h *Host) Serve(ctx context.Context, listener net.Listener) error {
	if h == nil || h.app == nil || ctx == nil || listener == nil {
		return ErrLocalConfiguration
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return ErrLocalConfiguration
	}
	_, port, err := net.SplitHostPort(h.origin.Host)
	if err != nil || port != strconv.Itoa(addr.Port) {
		return ErrLocalConfiguration
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				if _, err := h.worker.RunOne(workCtx); err != nil && workCtx.Err() == nil {
					done <- ErrLocalDependency
					cancel()
					return
				}
			}
		}
	}()
	serveErr := h.app.Serve(workCtx, listener)
	cancel()
	workerErr := <-done
	return errors.Join(serveErr, workerErr)
}
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	h.closeOnce.Do(func() {
		if h.mailbox != nil {
			h.closeErr = errors.Join(h.closeErr, h.mailbox.Close())
		}
		if h.pool != nil {
			if err := h.pool.Close(); err != nil {
				h.closeErr = errors.Join(h.closeErr, ErrLocalDependency)
			}
		}
		if h.db != nil {
			h.closeErr = errors.Join(h.closeErr, h.db.Close())
		}
	})
	return h.closeErr
}

// schemaReady verifies the optional durable assurance/factor protections before
// the host advertises readiness; a successful database ping is insufficient.
func (h *Host) schemaReady(ctx context.Context) error {
	return h.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		var sessions, factors int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM identity_sessions WHERE false AND assurance_level IS NULL AND assurance_expires_at IS NULL),(SELECT count(*) FROM identity_totp_factors WHERE false)`).Scan(&sessions, &factors); err != nil {
			return ErrLocalDependency
		}
		var protected bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='identity_auth_limits'::regclass AND conname='identity_auth_limits_operation_check' AND position('''mfa''' IN pg_get_constraintdef(oid))>0)`).Scan(&protected); err != nil || !protected {
			return ErrLocalDependency
		}
		return nil
	})
}
