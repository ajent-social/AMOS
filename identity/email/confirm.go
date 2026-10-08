package email

import (
	"encoding/json"
	"errors"
	"github.com/ajent-social/amos/identity/store"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
)

// ConfirmHandler exposes strict JSON confirmation for a browser form adapter.
// It must be wrapped by the deployment's durable Verification admission gate.
// GET previews remain read-only and are exposed separately by Preview.
func (s *Service) ConfirmHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setPrivateHeaders(w)
		if s == nil || (s.db == nil && s.root == nil) || s.origin == nil {
			confirmationError(w, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			confirmationError(w, http.StatusMethodNotAllowed, "method.not_allowed")
			return
		}
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != s.origin.String() {
			confirmationError(w, http.StatusForbidden, "request.origin_denied")
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || len(r.Header.Values("Content-Type")) != 1 || r.URL.RawQuery != "" {
			confirmationError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxConfirmBodyBytes))
		first, err := decoder.Token()
		if err != nil || first != json.Delim('{') {
			confirmationError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		fields := make(map[string]string, 2)
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				confirmationError(w, http.StatusBadRequest, "request.invalid")
				return
			}
			key, ok := name.(string)
			if !ok || (key != "challenge_id" && key != "token") {
				confirmationError(w, http.StatusBadRequest, "request.invalid")
				return
			}
			if _, found := fields[key]; found {
				confirmationError(w, http.StatusBadRequest, "request.invalid")
				return
			}
			var value string
			if decoder.Decode(&value) != nil {
				confirmationError(w, http.StatusBadRequest, "request.invalid")
				return
			}
			fields[key] = value
		}
		last, err := decoder.Token()
		if err != nil || last != json.Delim('}') || len(fields) != 2 || decoder.Decode(new(any)) != io.EOF {
			confirmationError(w, http.StatusBadRequest, "request.invalid")
			return
		}
		id, err := parseID(fields["challenge_id"])
		if err != nil {
			confirmationError(w, http.StatusUnauthorized, "identity.verification_unavailable")
			return
		}
		err = s.Confirm(r.Context(), id, fields["token"])
		if errors.Is(err, ErrChallengeUnavailable) || errors.Is(err, ErrInvalidRequest) {
			confirmationError(w, http.StatusUnauthorized, "identity.verification_unavailable")
			return
		}
		if err != nil {
			confirmationError(w, http.StatusServiceUnavailable, "dependency.unavailable")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func confirmationError(w http.ResponseWriter, status int, code string) {
	id := w.Header().Get("X-Request-ID")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || parsed.Version() != 7 || parsed.Variant() != uuid.RFC4122 {
		generated, err := store.NewID()
		if err != nil {
			id = "unavailable"
		} else {
			id = generated.String()
		}
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"id"`
	}{code, "Email verification could not be completed.", id})
}
