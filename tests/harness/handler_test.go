package harness

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// newBoundaryHandler is deliberately test-only. It is a real net/http boundary
// fixture and does not represent an AMOS production route or provider.
func newBoundaryHandler(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_test/harness", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"component":"api-harness-fixture","status":"ready"}`)
	})
	mux.HandleFunc("POST /_test/harness/records", func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer fixture-only-token" {
			writeJSON(w, http.StatusUnauthorized, `{"error":"authentication_required"}`)
			return
		}
		if db == nil {
			writeJSON(w, http.StatusServiceUnavailable, `{"error":"integration_database_required"}`)
			return
		}

		var input struct {
			Body string `json:"body"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Body) == "" {
			writeJSON(w, http.StatusBadRequest, `{"error":"invalid_request"}`)
			return
		}
		if err := db.QueryRowContext(request.Context(), "INSERT INTO harness_records (body) VALUES ($1) RETURNING body", input.Body).Scan(&input.Body); err != nil {
			writeJSON(w, http.StatusInternalServerError, `{"error":"persistence_failed"}`)
			return
		}
		writeJSON(w, http.StatusCreated, `{"persisted":true}`)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body+"\n"); err != nil {
		log.Printf("write test-only HTTP fixture response: %v", err)
	}
}
