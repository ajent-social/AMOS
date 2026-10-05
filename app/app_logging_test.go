package app_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajent-social/amos/app"
)

func TestAppForwardsConfiguredLoggerToRuntime(t *testing.T) {
	var output bytes.Buffer
	application, err := app.New(app.Options{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.RegisterBusinessRoute(http.MethodGet, "/orders/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/orders/42", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.Code)
	}
	var record struct {
		Route  string `json:"route"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Route != "/orders/*" || record.Status != http.StatusAccepted {
		t.Fatalf("App did not forward its logger to Runtime: %+v", record)
	}
}
