package apphost

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/ajent-social/amos/identity/protection"
	"github.com/google/uuid"
)

func (h *Host) bind(ctx context.Context, cfg LocalConfig) error {
	err := h.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		// Serialize deployment binding while retaining all pre-existing records.
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(406771290103)"); err != nil {
			return ErrLocalDependency
		}
		var foreign bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(
   SELECT 1 FROM identity_persons WHERE installation_id<>$1 OR application_id<>$2
   UNION ALL SELECT 1 FROM workspaces WHERE installation_id<>$1 OR application_id<>$2
   UNION ALL SELECT 1 FROM amos_jobs WHERE installation_id<>$1 OR application_id<>$2
   UNION ALL SELECT 1 FROM identity_sessions WHERE installation_id<>$1 OR application_id<>$2 OR environment_id<>$3
   UNION ALL SELECT 1 FROM identity_auth_limits WHERE installation_id<>$1 OR application_id<>$2 OR environment_id<>$3
   UNION ALL SELECT 1 FROM email_delivery_material WHERE installation_id<>$1 OR application_id<>$2 OR environment_id<>$3
   UNION ALL SELECT 1 FROM billing_verified_webhook_ingress WHERE installation_id<>$1 OR application_id<>$2 OR environment_id<>$3
   UNION ALL SELECT 1 FROM billing_workspace_accounts WHERE installation_id<>$1 OR application_id<>$2 OR environment_id<>$3
  )`, cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID).Scan(&foreign)
		if err != nil {
			return ErrLocalDependency
		}
		if foreign {
			return ErrLocalConfiguration
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO amos_runtime_binding(singleton,installation_id,application_id,environment_id) VALUES(true,$1,$2,$3) ON CONFLICT DO NOTHING", cfg.InstallationID, cfg.ApplicationID, cfg.EnvironmentID); err != nil {
			return ErrLocalDependency
		}
		var installation, application, environment uuid.UUID
		if err := tx.QueryRowContext(ctx, "SELECT installation_id,application_id,environment_id FROM amos_runtime_binding WHERE singleton=true").Scan(&installation, &application, &environment); err != nil {
			return ErrLocalDependency
		}
		if installation != cfg.InstallationID || application != cfg.ApplicationID || environment != cfg.EnvironmentID {
			return ErrLocalConfiguration
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func verificationAdmission(limiter *protection.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		peer, err := protection.PeerIP(r)
		if err != nil {
			http.Error(w, "Request unavailable.", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		admission, err := limiter.Allow(ctx, protection.Verification, peer, "peer:"+peer)
		if err != nil {
			http.Error(w, "Verification temporarily unavailable.", http.StatusServiceUnavailable)
			return
		}
		if !admission.Allowed {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Please try again later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
