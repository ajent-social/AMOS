package email

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/identity/store"
)

func TestJSONConfirmationRejectsAmbiguousProofAndConsumesOnce(t *testing.T) {
	db := newEmailDB(t)
	installation, application := emailID(t), emailID(t)
	account, token := createEmailAccount(t, db, installation, application, store.ChallengeEmailVerification, time.Hour)
	svc, err := New(db, nil, nil, nil, Config{InstallationID: installation, ApplicationID: application, ApplicationOrigin: "https://app.example.test", ChallengeLifetime: DefaultChallengeLifetime})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"challenge_id": account.ChallengeID.String(), "token": token})
	if err != nil {
		t.Fatal(err)
	}
	send := func(payload, origin string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/verify-email", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		svc.ConfirmHandler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("confirmation status=%d want=%d", w.Code, want)
		}
	}
	send(string(body), "https://foreign.example", http.StatusForbidden)
	send(`{"challenge_id":"invalid","challenge_id":"duplicate","token":"synthetic"}`, "https://app.example.test", http.StatusBadRequest)
	send(string(body)+`{}`, "https://app.example.test", http.StatusBadRequest)
	send(string(body), "https://app.example.test", http.StatusNoContent)
	send(string(body), "https://app.example.test", http.StatusUnauthorized)
}
