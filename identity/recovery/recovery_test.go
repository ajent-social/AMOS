package recovery

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity"
	"github.com/ajent-social/amos/identity/internal/authproof"
	"github.com/ajent-social/amos/identity/password"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

const (
	oldPassword = "correct horse battery staple 2026"
	newPassword = "different correct horse battery 2026"
)

func TestT3_8_ResetConsumesProofOnceAndRevokesSessions(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	account := activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, true)
	challengeID, token := issueResetChallenge(t, db, account, time.Hour)

	if response := completeResetHTTP(t, service, challengeID, token, newPassword); response.Code != http.StatusNoContent {
		t.Fatalf("complete reset status=%d body=%s", response.Code, response.Body.String())
	}
	if response := completeResetHTTP(t, service, challengeID, token, "third changed password 2026"); response.Code != http.StatusUnauthorized {
		t.Fatalf("replayed completion status=%d body=%s, want generic unauthorized", response.Code, response.Body.String())
	}
	var verifier string
	var epoch int64
	var revoked sql.NullTime
	var sessions, openChallenges int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT c.verifier_hash,p.security_epoch FROM identity_credentials c JOIN identity_persons p ON p.id=c.person_id WHERE p.id=$1`, account.personID).Scan(&verifier, &epoch); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT revoked_at FROM identity_sessions WHERE person_id=$1`, account.personID).Scan(&revoked); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM identity_sessions WHERE person_id=$1`, account.personID).Scan(&sessions); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM identity_challenges WHERE id=$1 AND consumed_at IS NULL`, challengeID).Scan(&openChallenges)
	}); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 || !revoked.Valid || sessions != 1 || openChallenges != 0 {
		t.Fatalf("durable transition epoch=%d revoked=%v sessions=%d openChallenges=%d", epoch, revoked.Valid, sessions, openChallenges)
	}
	if result, err := service.cfg.Passwords.Verify(context.Background(), "verify-old", oldPassword, verifier, nil); err != nil || result.Verified {
		t.Fatalf("old password remained valid: verified=%v err=%v", result.Verified, err)
	}
	if result, err := service.cfg.Passwords.Verify(context.Background(), "verify-new", newPassword, verifier, nil); err != nil || !result.Verified {
		t.Fatalf("new password not valid: verified=%v err=%v", result.Verified, err)
	}
}

func TestT3_8_RequestIsGenericAndStoresProtectedPurposeBoundIntent(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	account := activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, false)
	request := func(address string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(`{"email":"`+address+`"}`))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		service.RequestHandler().ServeHTTP(response, req)
		return response
	}
	known := request(" PERSON@example.test ")
	unknown := request("missing@example.test")
	if known.Code != http.StatusAccepted || unknown.Code != known.Code {
		t.Fatalf("recovery request statuses known=%d unknown=%d", known.Code, unknown.Code)
	}
	var knownResponse, unknownResponse response
	if err := json.Unmarshal(known.Body.Bytes(), &knownResponse); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknown.Body.Bytes(), &unknownResponse); err != nil {
		t.Fatal(err)
	}
	if knownResponse.Code != unknownResponse.Code || knownResponse.Message != unknownResponse.Message || knownResponse.Code != "identity.password-recovery.accepted" {
		t.Fatalf("request disclosure differs: known=%+v unknown=%+v", knownResponse, unknownResponse)
	}
	var challengeID uuid.UUID
	var materialID uuid.UUID
	var challengeCount, materialCount, jobCount int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT id FROM identity_challenges WHERE purpose='password_reset'`).Scan(&challengeID); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT material_id FROM email_delivery_material`).Scan(&materialID); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM identity_challenges WHERE purpose='password_reset'`).Scan(&challengeCount); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM email_delivery_material`).Scan(&materialCount); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM amos_jobs`).Scan(&jobCount)
	}); err != nil {
		t.Fatal(err)
	}
	if challengeCount != 1 || materialCount != 1 || jobCount != 1 || materialID != challengeID {
		t.Fatalf("intent durability challenges=%d materials=%d jobs=%d materialIDMatches=%v", challengeCount, materialCount, jobCount, materialID == challengeID)
	}
	materials := service.cfg.Materials.(*materialstore.Store)
	material, err := materials.ResolveForTemplate(context.Background(), deliveryemail.SecretReference("material:"+challengeID.String()), deliveryemail.TemplatePasswordReset)
	if err != nil {
		t.Fatalf("resolve protected reset material: %v", err)
	}
	action, err := url.Parse(material.ActionURL)
	if err != nil || action.Path != "/reset-password" || action.Query().Get("challenge") != challengeID.String() || action.Query().Get("token") == "" {
		t.Fatalf("reset action URL is not purpose-bound: %v", err)
	}
	var payload string
	var ciphertext []byte
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT payload::text FROM amos_jobs`).Scan(&payload); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT ciphertext FROM email_delivery_material WHERE material_id=$1`, challengeID).Scan(&ciphertext)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, action.Query().Get("token")) || strings.Contains(string(ciphertext), action.Query().Get("token")) {
		t.Fatal("reset token was persisted outside encrypted protected material")
	}
	preview := httptest.NewRecorder()
	previewReq := httptest.NewRequest(http.MethodGet, material.ActionURL, nil)
	service.PreviewHandler().ServeHTTP(preview, previewReq)
	if preview.Code != http.StatusOK || preview.Header().Get("Cache-Control") != "no-store" || preview.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("preview status=%d cache=%q referrer=%q", preview.Code, preview.Header().Get("Cache-Control"), preview.Header().Get("Referrer-Policy"))
	}
	var consumed sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT consumed_at FROM identity_challenges WHERE id=$1`, challengeID).Scan(&consumed)
	}); err != nil {
		t.Fatal(err)
	}
	if consumed.Valid {
		t.Fatal("preview consumed reset proof")
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, account.personID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	disabled := request("person@example.test")
	if disabled.Code != known.Code {
		t.Fatalf("disabled-account request status=%d, want same generic %d", disabled.Code, known.Code)
	}
	var disabledResponse response
	if err := json.Unmarshal(disabled.Body.Bytes(), &disabledResponse); err != nil {
		t.Fatal(err)
	}
	if disabledResponse.Code != knownResponse.Code || disabledResponse.Message != knownResponse.Message {
		t.Fatalf("disabled-account request disclosed eligibility: %+v", disabledResponse)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE identity_persons SET state='active' WHERE id=$1`, account.personID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE identity_emails SET verified_at=NULL WHERE id=$1`, account.emailID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	unverified := request("person@example.test")
	if unverified.Code != known.Code {
		t.Fatalf("unverified-account request status=%d, want same generic %d", unverified.Code, known.Code)
	}
	var unverifiedResponse response
	if err := json.Unmarshal(unverified.Body.Bytes(), &unverifiedResponse); err != nil {
		t.Fatal(err)
	}
	if unverifiedResponse.Code != knownResponse.Code || unverifiedResponse.Message != knownResponse.Message {
		t.Fatalf("unverified-account request disclosed eligibility: %+v", unverifiedResponse)
	}
}

func TestT3_8_RequiredStorageOutageFailsBeforeEligibilityLookup(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, false)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TABLE amos_jobs`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	call := func(address string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/forgot-password", strings.NewReader(`{"email":"`+address+`"}`))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		service.RequestHandler().ServeHTTP(response, req)
		return response
	}
	known := call("person@example.test")
	unknown := call("missing@example.test")
	if known.Code != http.StatusServiceUnavailable || unknown.Code != known.Code {
		t.Fatalf("storage outage statuses known=%d unknown=%d", known.Code, unknown.Code)
	}
	var knownResponse, unknownResponse response
	if err := json.Unmarshal(known.Body.Bytes(), &knownResponse); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknown.Body.Bytes(), &unknownResponse); err != nil {
		t.Fatal(err)
	}
	if knownResponse.Code != unknownResponse.Code || knownResponse.Message != unknownResponse.Message || knownResponse.Code != "dependency.unavailable" {
		t.Fatalf("storage outage disclosed eligibility: known=%+v unknown=%+v", knownResponse, unknownResponse)
	}
}

func TestT3_8_ExpiredAndDisabledResetProofsDoNotMutate(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	account := activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, false)
	expiredID, expiredToken := issueResetChallenge(t, db, account, -time.Minute)
	if err := service.complete(context.Background(), expiredID, digestToken(t, expiredToken), newPassword); !errors.Is(err, ErrChallengeUnavailable) {
		t.Fatalf("expired reset error=%v", err)
	}
	disabledID, disabledToken := issueResetChallenge(t, db, account, time.Hour)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, account.personID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.complete(context.Background(), disabledID, digestToken(t, disabledToken), newPassword); !errors.Is(err, ErrChallengeUnavailable) {
		t.Fatalf("disabled account reset error=%v", err)
	}
	var epoch int64
	var consumed sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT security_epoch FROM identity_persons WHERE id=$1`, account.personID).Scan(&epoch); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT consumed_at FROM identity_challenges WHERE id=$1`, disabledID).Scan(&consumed)
	}); err != nil {
		t.Fatal(err)
	}
	if epoch != 0 || consumed.Valid {
		t.Fatalf("invalid proof mutated state: epoch=%d consumed=%v", epoch, consumed.Valid)
	}
}

func TestT3_8_ConcurrentResetReplayHasOneWinner(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	account := activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, false)
	challengeID, token := issueResetChallenge(t, db, account, time.Hour)
	digest := digestToken(t, token)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- service.complete(context.Background(), challengeID, digest, newPassword)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, unavailable int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrChallengeUnavailable):
			unavailable++
		default:
			t.Fatalf("concurrent completion error=%v", err)
		}
	}
	if successes != 1 || unavailable != 1 {
		t.Fatalf("concurrent reset outcomes success=%d unavailable=%d", successes, unavailable)
	}
	var epoch int64
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT security_epoch FROM identity_persons WHERE id=$1`, account.personID).Scan(&epoch)
	}); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 {
		t.Fatalf("security epoch=%d, want exactly one transition", epoch)
	}
}

func TestT3_8_PasswordChangeRequiresCurrentPasswordAndRevokesSessions(t *testing.T) {
	db, service, ids := recoveryFixture(t)
	account := activeRecoveryAccount(t, db, service, ids, "person@example.test", oldPassword, true)
	proof, err := authproof.NewVerifiedCredential(account.personID, ids.installation, ids.application, ids.environment, 0, "email_password", time.Now().UTC().Add(-time.Minute), "aal1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.ContextWithVerifiedCredential(context.Background(), proof)
	call := func(current string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(`{"current_password":"`+current+`","new_password":"`+newPassword+`"}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		service.PasswordChangeHandler().ServeHTTP(response, req)
		return response
	}
	if response := call("incorrect current password phrase"); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status=%d body=%s", response.Code, response.Body.String())
	}
	if response := call(oldPassword); response.Code != http.StatusNoContent {
		t.Fatalf("password change status=%d body=%s", response.Code, response.Body.String())
	}
	var epoch int64
	var revoked sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT security_epoch FROM identity_persons WHERE id=$1`, account.personID).Scan(&epoch); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT revoked_at FROM identity_sessions WHERE person_id=$1`, account.personID).Scan(&revoked)
	}); err != nil {
		t.Fatal(err)
	}
	if epoch != 1 || !revoked.Valid {
		t.Fatalf("password change epoch=%d revoked=%v", epoch, revoked.Valid)
	}
}

type recoveryAccount struct{ personID, emailID uuid.UUID }

func recoveryFixture(t *testing.T) (*storage.DB, *Service, struct{ installation, application, environment uuid.UUID }) {
	t.Helper()
	db, raw := recoveryDB(t)
	ids := struct{ installation, application, environment uuid.UUID }{newRecoveryID(t), newRecoveryID(t), newRecoveryID(t)}
	hasher, err := password.New(recoveryBlocklist{}, recoveryBudget{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 4096, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	materials, err := materialstore.New(materialstore.Config{DB: db, InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, ActiveKeyID: "test", Keys: map[string][]byte{"test": bytes32(11)}, ApplicationOrigin: "https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{DB: db, Passwords: hasher, Outbox: outbox, Renderer: renderer, Materials: materials, Policy: allowRecoveryPolicy{}, InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, ApplicationOrigin: "https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, ids
}

func recoveryDB(t *testing.T) (*storage.DB, *sql.DB) {
	t.Helper()
	raw, schema := testkit.NewPostgres(t)
	raw.SetMaxOpenConns(1)
	if _, err := raw.ExecContext(context.Background(), `SET search_path TO "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	dsn, err := testkit.DatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = raw.Close() })
	fragments := make([]migrations.Fragment, 0, 3)
	for i, item := range []struct{ ns, file string }{{"identity", "identity.sql"}, {"jobs", "jobs.sql"}, {"email", "email-material.sql"}} {
		sqlText, err := os.ReadFile(filepath.Join("..", "..", "migrations", "fragments", item.file))
		if err != nil {
			t.Fatal("read required test migration")
		}
		fragments = append(fragments, migrations.Fragment{Namespace: item.ns, Migrations: []migrations.Migration{{Sequence: uint64(i + 1), Name: item.ns + "_base", SQL: string(sqlText)}}})
	}
	registry, err := migrations.NewRegistry(fragments...)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	return db, raw
}

func activeRecoveryAccount(t *testing.T, db *storage.DB, svc *Service, ids struct{ installation, application, environment uuid.UUID }, address, secret string, withSession bool) recoveryAccount {
	t.Helper()
	personID, emailID, credentialID := newRecoveryID(t), newRecoveryID(t), newRecoveryID(t)
	hash, err := svc.cfg.Passwords.Hash(context.Background(), "fixture", secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,'active')`, personID, ids.installation, ids.application); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,$5,$6,transaction_timestamp())`, emailID, personID, ids.installation, ids.application, address, address); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO identity_credentials(id,person_id,method,verifier_hash) VALUES($1,$2,'email_password',$3)`, credentialID, personID, hash); err != nil {
			return err
		}
		if withSession {
			sessionID := newRecoveryID(t)
			tokenDigest := make([]byte, 32)
			if _, err := rand.Read(tokenDigest); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO identity_sessions(id,person_id,installation_id,application_id,environment_id,token_digest,security_epoch,authentication_method,authenticated_at,expires_at,idle_expires_at) VALUES($1,$2,$3,$4,$5,$6,0,'email_password',transaction_timestamp(),transaction_timestamp()+interval '1 hour',transaction_timestamp()+interval '30 minutes')`, sessionID, personID, ids.installation, ids.application, ids.environment, tokenDigest)
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return recoveryAccount{personID: personID, emailID: emailID}
}

func issueResetChallenge(t *testing.T, db *storage.DB, account recoveryAccount, lifetime time.Duration) (uuid.UUID, string) {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	token, digest, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		createdAt := "transaction_timestamp()"
		if lifetime < 0 {
			createdAt = "transaction_timestamp()+($5 * interval '1 second')-interval '1 microsecond'"
		}
		query := `INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,created_at,expires_at) VALUES($1,$2,$3,'password_reset',$4,` + createdAt + `,transaction_timestamp()+($5 * interval '1 second'))`
		_, err := tx.Exec(query, id, account.personID, account.emailID, digest[:], lifetime.Seconds())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id, token
}

func newRecoveryID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func digestToken(t *testing.T, token string) []byte {
	t.Helper()
	_, digest, err := parseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	return digest[:]
}

func completeResetHTTP(t *testing.T, service *Service, challengeID uuid.UUID, token, next string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"challenge_id":"` + challengeID.String() + `","token":"` + token + `","password":"` + next + `"}`
	req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	service.CompleteHandler().ServeHTTP(rec, req)
	return rec
}

func bytes32(seed byte) []byte {
	return []byte{seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed, seed}
}

type recoveryBlocklist struct{}

func (recoveryBlocklist) Ready() bool                    { return true }
func (recoveryBlocklist) ContainsNormalized(string) bool { return false }

type recoveryBudget struct{}

func (recoveryBudget) Allow(context.Context, string) error { return nil }

type allowRecoveryPolicy struct{}

func (allowRecoveryPolicy) AuthorizePasswordChange(context.Context, *sql.Tx, identity.Principal) error {
	return nil
}
func (allowRecoveryPolicy) AuthorizePasswordReset(context.Context, *sql.Tx, uuid.UUID) error {
	return nil
}
