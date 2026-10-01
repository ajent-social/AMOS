package magiclink

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	deliveryemail "github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/materialstore"
	"github.com/ajent-social/amos/identity/session"
	"github.com/ajent-social/amos/identity/store"
	"github.com/ajent-social/amos/internal/testkit"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type allowMagicPolicy struct{}

func (allowMagicPolicy) Ready(context.Context, *sql.Tx) error                         { return nil }
func (allowMagicPolicy) AuthorizeMagicLink(context.Context, *sql.Tx, uuid.UUID) error { return nil }

func TestT3_9_TokenAndAddressParsingAreCanonical(t *testing.T) {
	token, digest, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	parsed, parsedDigest, err := parseToken(token)
	if err != nil || parsed != token || digest != parsedDigest {
		t.Fatalf("canonical token did not round-trip: err=%v", err)
	}
	for _, invalid := range []string{token + "=", token[:42] + "!", "short"} {
		if _, _, err := parseToken(invalid); err == nil {
			t.Fatalf("accepted malformed token %q", invalid)
		}
	}
	for input, expected := range map[string]string{
		"Person@example.test":     "person@example.test",
		"person+tag@EXAMPLE.test": "person+tag@example.test",
	} {
		got, ok := normalizeAddress(input)
		if !ok || got != expected {
			t.Fatalf("normalizeAddress(%q)=(%q,%v), want %q", input, got, ok, expected)
		}
	}
	for _, input := range []string{"Person <person@example.test>", "person@éxample.test", "bad@@example.test", "a\nb@example.test"} {
		if _, ok := normalizeAddress(input); ok {
			t.Fatalf("accepted invalid address %q", input)
		}
	}
}

func TestT3_9_BrowserCookieNamespaceIsBoundedAndRejectsDuplicates(t *testing.T) {
	service := &Service{cookieNS: productionCookiePrefix}
	var pairs []string
	for range MaxBrowserFlows {
		id, err := store.NewID()
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, service.flowCookieName(id)+"="+token)
	}
	request := httptest.NewRequest("POST", "/", nil)
	request.Header.Set("Cookie", strings.Join(pairs, "; "))
	cookies, err := service.browserCookies(request)
	if err != nil || len(cookies) != MaxBrowserFlows {
		t.Fatalf("four owned flows: count=%d err=%v", len(cookies), err)
	}

	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	tooMany := httptest.NewRequest("POST", "/", nil)
	tooMany.Header.Set("Cookie", strings.Join(append(pairs, service.flowCookieName(id)+"="+token), "; "))
	if _, err := service.browserCookies(tooMany); err != ErrTooManyBrowserFlows {
		t.Fatalf("fifth flow error=%v, want ErrTooManyBrowserFlows", err)
	}

	duplicate := httptest.NewRequest("POST", "/", nil)
	duplicate.Header.Set("Cookie", pairs[0]+"; "+pairs[0])
	if _, err := service.browserCookies(duplicate); err == nil {
		t.Fatal("duplicate owned cookie was accepted")
	}
	foreign := httptest.NewRequest("POST", "/", nil)
	foreign.Header.Set("Cookie", developmentCookiePrefix+strings.TrimPrefix(strings.SplitN(pairs[0], "=", 2)[0], productionCookiePrefix)+"="+token)
	if _, err := service.browserCookies(foreign); err == nil {
		t.Fatal("cookie from the other environment namespace was accepted")
	}
}

func TestT3_9_FlowCookiesAreHostScopedAndExpireWithinChallengeLifetime(t *testing.T) {
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{cookieNS: productionCookiePrefix}
	cookie := service.flowCookie(id, token, 12*time.Minute)
	if !strings.HasPrefix(cookie.Name, "__Host-") || cookie.Path != "/" || cookie.Domain != "" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 720 {
		t.Fatalf("unexpected production flow cookie attributes: %+v", cookie)
	}
	dev := &Service{cookieNS: developmentCookiePrefix, cfg: Config{DevelopmentLoopback: true}}
	devCookie := dev.flowCookie(id, token, 12*time.Minute)
	if devCookie.Secure || devCookie.Domain != "" || devCookie.Path != "/" || !devCookie.HttpOnly {
		t.Fatalf("unexpected explicit development cookie attributes: %+v", devCookie)
	}
}

func TestT3_9_RealPostgresPreviewDoesNotConsumeAndConfirmationIsSingleUse(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "person@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	service, materials := magicService(t, db, raw, ids, allowMagicPolicy{})
	unsafeIssue := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"person@example.test"}`))
	unsafeIssue.Header.Set("Content-Type", "application/json")
	unsafeIssue.Header.Set("Origin", "https://attacker.example.test")
	unsafeIssueResult := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(unsafeIssueResult, unsafeIssue)
	if unsafeIssueResult.Code != http.StatusForbidden || unsafeIssueResult.Header().Get("Set-Cookie") != "" || magicChallengeCount(t, db, personID) != 0 {
		t.Fatal("cross-origin issue request was accepted or persisted")
	}
	issue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"PERSON@example.test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		res := httptest.NewRecorder()
		service.RequestHandler().ServeHTTP(res, req)
		return res
	}
	firstRequest := issue()
	if firstRequest.Code != http.StatusAccepted || len(firstRequest.Result().Cookies()) != 1 {
		t.Fatalf("issue status=%d cookies=%d body=%s", firstRequest.Code, len(firstRequest.Result().Cookies()), firstRequest.Body.String())
	}
	action := latestMagicAction(t, db, materials)
	challengeID, token := actionChallenge(t, action)
	var queued, leaked int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT count(*) FROM amos_jobs WHERE kind=$1 AND external_effect=true`, deliveryemail.Kind).Scan(&queued); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM amos_jobs WHERE payload::text LIKE '%'||$1||'%'`, token).Scan(&leaked)
	}); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || leaked != 0 {
		t.Fatalf("outbox jobs=%d plaintext token matches=%d", queued, leaked)
	}
	firstFlowCookie := firstRequest.Result().Cookies()[0]
	previewURL := previewPath(t, action)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, previewURL, nil)
		req.AddCookie(firstFlowCookie)
		res := httptest.NewRecorder()
		service.PreviewHandler().ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Referrer-Policy") != "no-referrer" || res.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s preview status/headers=%d %v", method, res.Code, res.Header())
		}
		if method == http.MethodHead && res.Body.Len() != 0 {
			t.Fatal("HEAD preview returned a body")
		}
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	confirm := func(id uuid.UUID, secret string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		body, _ := json.Marshal(confirmRequest{ChallengeID: id.String(), Token: secret})
		req := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		service.ConfirmHandler().ServeHTTP(res, req)
		return res
	}
	wrongBrowser := confirm(challengeID, token)
	if wrongBrowser.Code != http.StatusForbidden {
		t.Fatalf("implicit cross-device confirmation=%d body=%s", wrongBrowser.Code, wrongBrowser.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	crossOriginBody, _ := json.Marshal(confirmRequest{ChallengeID: challengeID.String(), Token: token})
	crossOriginRequest := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(crossOriginBody)))
	crossOriginRequest.Header.Set("Content-Type", "application/json")
	crossOriginRequest.Header.Set("Origin", "https://attacker.example.test")
	crossOriginRequest.AddCookie(firstFlowCookie)
	crossOriginResult := httptest.NewRecorder()
	service.ConfirmHandler().ServeHTTP(crossOriginResult, crossOriginRequest)
	if crossOriginResult.Code != http.StatusForbidden {
		t.Fatalf("cross-origin confirmation=%d body=%s", crossOriginResult.Code, crossOriginResult.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	confirmed := confirm(challengeID, token, firstFlowCookie)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var body struct {
		Authenticated bool   `json:"authenticated"`
		Assurance     string `json:"assurance"`
		CSRFToken     string `json:"csrf_token"`
	}
	if err := json.Unmarshal(confirmed.Body.Bytes(), &body); err != nil || !body.Authenticated || body.Assurance != "aal1" || body.CSRFToken == "" {
		t.Fatalf("invalid confirmation response %+v err=%v", body, err)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range confirmed.Result().Cookies() {
		if cookie.Name == "__Host-amos_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || strings.Contains(confirmed.Body.String(), sessionCookie.Value) {
		t.Fatal("session cookie missing or session token exposed in response body")
	}
	assertMagicDurableState(t, db, challengeID, true, 1)
	if method := magicSessionMethod(t, db, personID); method != "email_magic_link" {
		t.Fatalf("session method=%q", method)
	}
	if replay := confirm(challengeID, token, firstFlowCookie); replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, true, 1)

	secondRequest := issue()
	secondAction := latestMagicAction(t, db, materials)
	secondChallengeID, secondToken := actionChallenge(t, secondAction)
	secondFlowCookie := secondRequest.Result().Cookies()[0]
	parallelConfirm := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(confirmRequest{ChallengeID: secondChallengeID.String(), Token: secondToken})
		req := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		req.AddCookie(sessionCookie)
		req.AddCookie(secondFlowCookie)
		res := httptest.NewRecorder()
		service.ConfirmHandler().ServeHTTP(res, req)
		return res
	}
	start, results := make(chan struct{}), make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- parallelConfirm() }()
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result.Code == http.StatusOK {
			winners++
		} else if result.Code != http.StatusUnauthorized {
			t.Fatalf("concurrent confirmation status=%d body=%s", result.Code, result.Body.String())
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent confirmation winners=%d", winners)
	}
	assertMagicDurableState(t, db, secondChallengeID, true, 1)
}

func TestT3_9_RealPostgresCrossDeviceRequiresExplicitOriginCheckedPOST(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "person@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	service, materials := magicService(t, db, raw, ids, allowMagicPolicy{})
	issue := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"person@example.test"}`))
	issue.Header.Set("Content-Type", "application/json")
	issue.Header.Set("Origin", "https://app.example.test")
	issued := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(issued, issue)
	if issued.Code != http.StatusAccepted {
		t.Fatalf("issue=%d %s", issued.Code, issued.Body.String())
	}
	challengeID, token := actionChallenge(t, latestMagicAction(t, db, materials))
	post := func(explicit, crossOrigin bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(confirmRequest{ChallengeID: challengeID.String(), Token: token, ConfirmDifferentDevice: explicit})
		req := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		origin := "https://app.example.test"
		if crossOrigin {
			origin = "https://attacker.example.test"
		}
		req.Header.Set("Origin", origin)
		res := httptest.NewRecorder()
		service.ConfirmHandler().ServeHTTP(res, req)
		return res
	}
	if implicit := post(false, false); implicit.Code != http.StatusForbidden {
		t.Fatalf("implicit different-device confirmation=%d", implicit.Code)
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	if crossOrigin := post(true, true); crossOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin explicit confirmation=%d", crossOrigin.Code)
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	if explicit := post(true, false); explicit.Code != http.StatusOK {
		t.Fatalf("explicit different-device confirmation=%d %s", explicit.Code, explicit.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, true, 1)
}

func TestT3_9_RealPostgresConfirmRechecksPolicyBeforeConsume(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "person@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	policy := &switchMagicPolicy{}
	service, materials := magicService(t, db, raw, ids, policy)
	issue := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"person@example.test"}`))
	issue.Header.Set("Content-Type", "application/json")
	issue.Header.Set("Origin", "https://app.example.test")
	issued := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(issued, issue)
	if issued.Code != http.StatusAccepted {
		t.Fatalf("issue=%d %s", issued.Code, issued.Body.String())
	}
	challengeID, token := actionChallenge(t, latestMagicAction(t, db, materials))
	flowCookie := issued.Result().Cookies()[0]
	policy.setDenied(true)
	post := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(confirmRequest{ChallengeID: challengeID.String(), Token: token})
		req := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		req.AddCookie(flowCookie)
		res := httptest.NewRecorder()
		service.ConfirmHandler().ServeHTTP(res, req)
		return res
	}
	denied := post()
	if denied.Code != http.StatusForbidden {
		t.Fatalf("policy denied confirmation=%d body=%s", denied.Code, denied.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	policy.setDenied(false)
	if allowed := post(); allowed.Code != http.StatusOK {
		t.Fatalf("policy-allowed confirmation=%d body=%s", allowed.Code, allowed.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, true, 1)
}

func TestT3_9_RealPostgresSessionFailureRollsBackChallengeAndCanRetry(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "person@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	service, materials := magicService(t, db, raw, ids, allowMagicPolicy{})
	issue := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"person@example.test"}`))
	issue.Header.Set("Content-Type", "application/json")
	issue.Header.Set("Origin", "https://app.example.test")
	issued := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(issued, issue)
	if issued.Code != http.StatusAccepted {
		t.Fatalf("issue=%d %s", issued.Code, issued.Body.String())
	}
	challengeID, token := actionChallenge(t, latestMagicAction(t, db, materials))
	flowCookie := issued.Result().Cookies()[0]
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE FUNCTION reject_magic_test_session() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced test session insert failure'; END $$`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`CREATE TRIGGER reject_magic_test_session BEFORE INSERT ON identity_sessions FOR EACH ROW EXECUTE FUNCTION reject_magic_test_session()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	confirm := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(confirmRequest{ChallengeID: challengeID.String(), Token: token})
		req := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		req.AddCookie(flowCookie)
		res := httptest.NewRecorder()
		service.ConfirmHandler().ServeHTTP(res, req)
		return res
	}
	failed := confirm()
	if failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("session insert failure=%d %s", failed.Code, failed.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, false, 0)
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER reject_magic_test_session ON identity_sessions`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`DROP FUNCTION reject_magic_test_session()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if retried := confirm(); retried.Code != http.StatusOK {
		t.Fatalf("retry after transaction rollback=%d %s", retried.Code, retried.Body.String())
	}
	assertMagicDurableState(t, db, challengeID, true, 1)
}

func TestT3_9_RealPostgresDisabledUnknownAndPolicyDeniedRequestsAreGeneric(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "disabled@example.test", "administratively_disabled"); err != nil {
		t.Fatal(err)
	}
	pendingPerson, pendingEmail := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, pendingPerson, pendingEmail, "pending@example.test", store.AccountPendingVerification); err != nil {
		t.Fatal(err)
	}
	service, _ := magicService(t, db, raw, ids, allowMagicPolicy{})
	call := func(svc *Service, address string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(issueRequest{Email: address})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://app.example.test")
		res := httptest.NewRecorder()
		svc.RequestHandler().ServeHTTP(res, req)
		return res
	}
	disabled, pending, unknown := call(service, "disabled@example.test"), call(service, "pending@example.test"), call(service, "unknown@example.test")
	var disabledBody, pendingBody, unknownBody response
	if err := json.Unmarshal(disabled.Body.Bytes(), &disabledBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknown.Body.Bytes(), &unknownBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(pending.Body.Bytes(), &pendingBody); err != nil {
		t.Fatal(err)
	}
	if disabled.Code != http.StatusAccepted || unknown.Code != disabled.Code || pending.Code != disabled.Code || disabledBody.Code != unknownBody.Code || disabledBody.Message != unknownBody.Message || pendingBody.Code != unknownBody.Code || pendingBody.Message != unknownBody.Message {
		t.Fatalf("disabled/pending/unknown differ: disabled=%d %+v pending=%d %+v unknown=%d %+v", disabled.Code, disabledBody, pending.Code, pendingBody, unknown.Code, unknownBody)
	}
	if count := magicChallengeCount(t, db, personID); count != 0 {
		t.Fatalf("disabled account challenges=%d", count)
	}
	if count := magicChallengeCount(t, db, pendingPerson); count != 0 {
		t.Fatalf("unverified account challenges=%d", count)
	}

	activePerson, activeEmail := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, activePerson, activeEmail, "policy@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	policyDenied, _ := magicService(t, db, raw, ids, denyMagicPolicy{})
	denied := call(policyDenied, "policy@example.test")
	if denied.Code != http.StatusAccepted || magicChallengeCount(t, db, activePerson) != 0 {
		t.Fatalf("policy-denied request status=%d challenges=%d", denied.Code, magicChallengeCount(t, db, activePerson))
	}

	unready, _ := magicService(t, db, raw, ids, unavailableMagicPolicy{})
	knownFailure, unknownFailure := call(unready, "policy@example.test"), call(unready, "absent@example.test")
	var knownBody, unknownReadyBody response
	if err := json.Unmarshal(knownFailure.Body.Bytes(), &knownBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknownFailure.Body.Bytes(), &unknownReadyBody); err != nil {
		t.Fatal(err)
	}
	if knownFailure.Code != http.StatusServiceUnavailable || unknownFailure.Code != knownFailure.Code || knownBody.Code != unknownReadyBody.Code || knownBody.Message != unknownReadyBody.Message {
		t.Fatalf("policy outage differs by account: known=%d %+v unknown=%d %+v", knownFailure.Code, knownBody, unknownFailure.Code, unknownReadyBody)
	}
	var flowCookies []string
	for range MaxBrowserFlows + 1 {
		flowID, err := store.NewID()
		if err != nil {
			t.Fatal(err)
		}
		flowToken, _, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		flowCookies = append(flowCookies, unready.flowCookieName(flowID)+"="+flowToken)
	}
	limitedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"policy@example.test"}`))
	limitedRequest.Header.Set("Content-Type", "application/json")
	limitedRequest.Header.Set("Origin", "https://app.example.test")
	limitedRequest.Header.Set("Cookie", strings.Join(flowCookies, "; "))
	limited := httptest.NewRecorder()
	unready.RequestHandler().ServeHTTP(limited, limitedRequest)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("over-capacity browser returned %d before unavailable policy check", limited.Code)
	}
}

func TestT3_9_RealPostgresExpiredAndCrossPurposeProofsCannotSignIn(t *testing.T) {
	db, raw := magicDB(t)
	ids := magicIDs{newMagicID(t), newMagicID(t), newMagicID(t)}
	personID, emailID := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, personID, emailID, "person@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	service, materials := magicService(t, db, raw, ids, allowMagicPolicy{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"person@example.test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://app.example.test")
	issued := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(issued, request)
	if issued.Code != http.StatusAccepted {
		t.Fatalf("issue=%d %s", issued.Code, issued.Body.String())
	}
	action := latestMagicAction(t, db, materials)
	challengeID, token := actionChallenge(t, action)
	flowCookie := issued.Result().Cookies()[0]
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_challenges SET expires_at=created_at+interval '1 microsecond' WHERE id=$1`, challengeID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	preview := httptest.NewRecorder()
	service.PreviewHandler().ServeHTTP(preview, httptest.NewRequest(http.MethodGet, previewPath(t, action), nil))
	if preview.Code != http.StatusOK || strings.Contains(preview.Body.String(), `action="/magic-link/confirm"`) {
		t.Fatal("expired challenge was presented as available")
	}
	expiredBody, _ := json.Marshal(confirmRequest{ChallengeID: challengeID.String(), Token: token})
	expiredRequest := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(expiredBody)))
	expiredRequest.Header.Set("Content-Type", "application/json")
	expiredRequest.Header.Set("Origin", "https://app.example.test")
	expiredRequest.AddCookie(flowCookie)
	expired := httptest.NewRecorder()
	service.ConfirmHandler().ServeHTTP(expired, expiredRequest)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired confirmation=%d %s", expired.Code, expired.Body.String())
	}

	crossPurposeID := newMagicID(t)
	crossPurposeToken, crossPurposeDigest, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO identity_challenges(id,person_id,email_id,purpose,token_digest,expires_at) VALUES($1,$2,$3,$4,$5,transaction_timestamp()+interval '10 minutes')`, crossPurposeID, personID, emailID, store.ChallengeEmailVerification, crossPurposeDigest[:])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	crossPurposeBody, _ := json.Marshal(confirmRequest{ChallengeID: crossPurposeID.String(), Token: crossPurposeToken, ConfirmDifferentDevice: true})
	crossPurposeRequest := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(crossPurposeBody)))
	crossPurposeRequest.Header.Set("Content-Type", "application/json")
	crossPurposeRequest.Header.Set("Origin", "https://app.example.test")
	crossPurpose := httptest.NewRecorder()
	service.ConfirmHandler().ServeHTTP(crossPurpose, crossPurposeRequest)
	if crossPurpose.Code != http.StatusUnauthorized {
		t.Fatalf("cross-purpose confirmation=%d %s", crossPurpose.Code, crossPurpose.Body.String())
	}
	var consumed sql.NullTime
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT consumed_at FROM identity_challenges WHERE id=$1`, crossPurposeID).Scan(&consumed)
	}); err != nil {
		t.Fatal(err)
	}
	if consumed.Valid || magicSessionCount(t, db, personID) != 0 {
		t.Fatal("cross-purpose challenge was consumed or issued a session")
	}

	disabledPerson, disabledEmail := newMagicID(t), newMagicID(t)
	if err := seedMagicAccount(db, ids, disabledPerson, disabledEmail, "disabled-after-issue@example.test", "active"); err != nil {
		t.Fatal(err)
	}
	disabledIssue := httptest.NewRequest(http.MethodPost, "/api/v1/identity/magic-links", strings.NewReader(`{"email":"disabled-after-issue@example.test"}`))
	disabledIssue.Header.Set("Content-Type", "application/json")
	disabledIssue.Header.Set("Origin", "https://app.example.test")
	disabledIssued := httptest.NewRecorder()
	service.RequestHandler().ServeHTTP(disabledIssued, disabledIssue)
	if disabledIssued.Code != http.StatusAccepted {
		t.Fatalf("active account issue=%d %s", disabledIssued.Code, disabledIssued.Body.String())
	}
	disabledChallengeID, disabledToken := actionChallenge(t, latestMagicAction(t, db, materials))
	disabledCookie := disabledIssued.Result().Cookies()[0]
	if err := db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE identity_persons SET state='administratively_disabled' WHERE id=$1`, disabledPerson)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	disabledBody, _ := json.Marshal(confirmRequest{ChallengeID: disabledChallengeID.String(), Token: disabledToken})
	disabledRequest := httptest.NewRequest(http.MethodPost, "/magic-link/confirm", strings.NewReader(string(disabledBody)))
	disabledRequest.Header.Set("Content-Type", "application/json")
	disabledRequest.Header.Set("Origin", "https://app.example.test")
	disabledRequest.AddCookie(disabledCookie)
	disabledResult := httptest.NewRecorder()
	service.ConfirmHandler().ServeHTTP(disabledResult, disabledRequest)
	if disabledResult.Code != http.StatusUnauthorized || magicSessionCount(t, db, disabledPerson) != 0 {
		t.Fatalf("disabled-after-issue confirmation=%d sessions=%d", disabledResult.Code, magicSessionCount(t, db, disabledPerson))
	}
	assertMagicDurableState(t, db, disabledChallengeID, false, 0)
}

type denyMagicPolicy struct{}

func (denyMagicPolicy) Ready(context.Context, *sql.Tx) error { return nil }
func (denyMagicPolicy) AuthorizeMagicLink(context.Context, *sql.Tx, uuid.UUID) error {
	return ErrStepUpRequired
}

type unavailableMagicPolicy struct{}

func (unavailableMagicPolicy) Ready(context.Context, *sql.Tx) error {
	return errors.New("test policy dependency unavailable")
}
func (unavailableMagicPolicy) AuthorizeMagicLink(context.Context, *sql.Tx, uuid.UUID) error {
	return nil
}

type switchMagicPolicy struct {
	mu     sync.Mutex
	denied bool
}

func (p *switchMagicPolicy) Ready(context.Context, *sql.Tx) error { return nil }
func (p *switchMagicPolicy) AuthorizeMagicLink(context.Context, *sql.Tx, uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.denied {
		return ErrStepUpRequired
	}
	return nil
}
func (p *switchMagicPolicy) setDenied(denied bool) {
	p.mu.Lock()
	p.denied = denied
	p.mu.Unlock()
}

type magicIDs struct{ installation, application, environment uuid.UUID }

func magicDB(t *testing.T) (*storage.DB, *sql.DB) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); _ = raw.Close() })
	registry, err := migrations.Core(
		migrations.Fragment{Namespace: "business", Migrations: []migrations.Migration{{Sequence: 8, Name: "test_business", SQL: "CREATE TABLE test_business (id uuid PRIMARY KEY);"}}},
		mustMagicMigration(t, 9), mustMagicMigration(t, 10), mustMagicMigration(t, 11),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(ctx, db, registry); err != nil {
		t.Fatal(err)
	}
	return db, raw
}

func mustMagicMigration(t *testing.T, sequence uint64) migrations.Fragment {
	t.Helper()
	switch sequence {
	case 9:
		fragment, err := migrations.BillingWebhookIngress(sequence)
		if err != nil {
			t.Fatal(err)
		}
		return fragment
	case 10:
		fragment, err := migrations.RuntimeBinding(sequence)
		if err != nil {
			t.Fatal(err)
		}
		return fragment
	case 11:
		fragment, err := migrations.MagicBrowserBinding(sequence)
		if err != nil {
			t.Fatal(err)
		}
		return fragment
	default:
		t.Fatal("unsupported migration sequence")
		return migrations.Fragment{}
	}
}

func magicService(t *testing.T, db *storage.DB, raw *sql.DB, ids magicIDs, policy Policy) (*Service, *materialstore.Store) {
	t.Helper()
	renderer, err := deliveryemail.NewRenderer(deliveryemail.RenderConfig{FromAddress: "no-reply@example.test", ApplicationOrigin: "https://app.example.test", MaxBodyBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	materials, err := materialstore.New(materialstore.Config{DB: db, InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, ActiveKeyID: "test-key", Keys: map[string][]byte{"test-key": key}, ApplicationOrigin: "https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := sqlstore.New(raw, sqlstore.Config{MaxPayloadBytes: 8192, MaxAttempts: 5, MaxReconciliationAttempts: 3, MaxLease: time.Minute, MaxRetryDelay: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(db, session.Config{InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, AllowedOrigins: []string{"https://app.example.test"}, CookieSecure: true})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{DB: db, Outbox: outbox, Renderer: renderer, Materials: materials, Sessions: sessions, Policy: policy, InstallationID: ids.installation, ApplicationID: ids.application, EnvironmentID: ids.environment, ApplicationOrigin: "https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return service, materials
}

func seedMagicAccount(db *storage.DB, ids magicIDs, personID, emailID uuid.UUID, address, state string) error {
	return db.WithTx(context.Background(), nil, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO identity_persons(id,installation_id,application_id,state) VALUES($1,$2,$3,$4)`, personID, ids.installation, ids.application, state); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO identity_emails(id,person_id,installation_id,application_id,display_address,comparison_key,verified_at) VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $7='active' THEN transaction_timestamp() ELSE NULL END)`, emailID, personID, ids.installation, ids.application, address, strings.ToLower(address), state)
		return err
	})
}

func latestMagicAction(t *testing.T, db *storage.DB, materials *materialstore.Store) deliveryemail.PrivateMaterial {
	t.Helper()
	var challengeID uuid.UUID
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT id FROM identity_challenges WHERE purpose=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, challengePurpose).Scan(&challengeID)
	}); err != nil {
		t.Fatal(err)
	}
	material, err := materials.ResolveForTemplate(context.Background(), deliveryemail.SecretReference("material:"+challengeID.String()), deliveryemail.TemplateSignIn)
	if err != nil {
		t.Fatal(err)
	}
	return material
}

func actionChallenge(t *testing.T, material deliveryemail.PrivateMaterial) (uuid.UUID, string) {
	t.Helper()
	action, err := url.Parse(material.ActionURL)
	if err != nil {
		t.Fatal(err)
	}
	query := action.Query()
	challengeID, ok := parseID(query.Get("challenge"))
	if !ok || len(query["challenge"]) != 1 || len(query["token"]) != 1 {
		t.Fatal("malformed magic-link action URL")
	}
	return challengeID, query.Get("token")
}

func previewPath(t *testing.T, material deliveryemail.PrivateMaterial) string {
	t.Helper()
	u, err := url.Parse(material.ActionURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.RequestURI()
}

func assertMagicDurableState(t *testing.T, db *storage.DB, challengeID uuid.UUID, consumed bool, activeSessions int) {
	t.Helper()
	var consumedAt sql.NullTime
	var count int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT consumed_at FROM identity_challenges WHERE id=$1`, challengeID).Scan(&consumedAt); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM identity_sessions WHERE revoked_at IS NULL`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if consumedAt.Valid != consumed || count != activeSessions {
		t.Fatalf("challenge consumed=%v want %v; active sessions=%d want %d", consumedAt.Valid, consumed, count, activeSessions)
	}
}

func magicSessionMethod(t *testing.T, db *storage.DB, personID uuid.UUID) string {
	t.Helper()
	var method string
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT authentication_method FROM identity_sessions WHERE person_id=$1 AND revoked_at IS NULL`, personID).Scan(&method)
	}); err != nil {
		t.Fatal(err)
	}
	return method
}

func magicSessionCount(t *testing.T, db *storage.DB, personID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_sessions WHERE person_id=$1`, personID).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func magicChallengeCount(t *testing.T, db *storage.DB, personID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.WithTx(context.Background(), &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT count(*) FROM identity_challenges WHERE person_id=$1 AND purpose=$2`, personID, challengePurpose).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func newMagicID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
