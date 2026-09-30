package harness

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoundaryFixture(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(newBoundaryHandler(nil))
	t.Cleanup(server.Close)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "route returns a real response",
			method:     http.MethodGet,
			path:       "/_test/harness",
			wantStatus: http.StatusOK,
			wantBody:   "{\"component\":\"api-harness-fixture\",\"status\":\"ready\"}\n",
		},
		{
			name:       "missing authentication is denied",
			method:     http.MethodPost,
			path:       "/_test/harness/records",
			body:       `{"body":"synthetic request"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   "{\"error\":\"authentication_required\"}\n",
		},
		{
			name:       "unregistered route returns not found",
			method:     http.MethodGet,
			path:       "/_test/unknown",
			wantStatus: http.StatusNotFound,
			wantBody:   "404 page not found\n",
		},
	}

	client := server.Client()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, strings.NewReader(test.body))
			if err != nil {
				t.Fatalf("build HTTP request: %v", err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("send HTTP request to test boundary: %v", err)
			}
			assertHTTPResponse(t, response, test.wantStatus, test.wantBody)
		})
	}
}

func assertHTTPResponse(t *testing.T, response *http.Response, wantStatus int, wantBody string) {
	t.Helper()
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close fixture response: %v", err)
		}
	}()
	if response.StatusCode != wantStatus {
		t.Fatalf("HTTP status = %d, want %d", response.StatusCode, wantStatus)
	}
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") && wantStatus != http.StatusNotFound {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read HTTP response body: %v", err)
	}
	if string(body) != wantBody {
		t.Fatalf("HTTP body = %q, want %q", string(body), wantBody)
	}
}
