package apphost

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/ui/authpassword"
)

func TestLocalAuthPageHeadIsReadOnly(t *testing.T) {
	for _, themed := range []bool{false, true} {
		name := "default"
		if themed {
			name = "themed"
		}
		t.Run(name, func(t *testing.T) {
			dsn := localDatabase(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			origin := "http://" + listener.Addr().String()
			cfg := localConfig(t, dsn, origin)
			if themed {
				cfg.AuthTheme = &authpassword.Theme{BrandName: "Example", LogoURL: "/assets/logo.svg", StylesheetURL: "/assets/auth.css"}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			host, err := NewLocal(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- host.Serve(ctx, listener) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					_ = listener.Close()
					t.Error("local host failed to stop")
				}
				if err := host.Close(); err != nil {
					t.Error(err)
				}
			})
			counts := func() [3]int64 {
				t.Helper()
				var v [3]int64
				err := host.pool.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM identity_persons), (SELECT count(*) FROM identity_sessions), (SELECT count(*) FROM identity_challenges)`).Scan(&v[0], &v[1], &v[2])
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			before := counts()
			client := &http.Client{Timeout: 5 * time.Second}
			for _, path := range []string{"/signup", "/signin"} {
				var getHeaders http.Header
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					req, err := http.NewRequestWithContext(ctx, method, origin+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					res, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, readErr := io.ReadAll(res.Body)
					closeErr := res.Body.Close()
					if readErr != nil || closeErr != nil {
						t.Fatalf("read response: %v, close: %v", readErr, closeErr)
					}
					if res.StatusCode != http.StatusOK {
						t.Fatalf("%s %s=%d", method, path, res.StatusCode)
					}
					if method == http.MethodGet {
						getHeaders = res.Header.Clone()
						if !strings.Contains(string(body), "<form") {
							t.Fatalf("GET %s missing form", path)
						}
						if themed && (!strings.Contains(string(body), "Example") || !strings.Contains(string(body), "/assets/auth.css")) {
							t.Fatalf("GET %s missing configured theme", path)
						}
					} else if len(body) != 0 {
						t.Fatalf("HEAD %s returned a body", path)
					}
					for _, header := range []string{"Content-Type", "Cache-Control", "Referrer-Policy", "X-Content-Type-Options", "Content-Security-Policy"} {
						value := res.Header.Get(header)
						if value == "" || value != getHeaders.Get(header) {
							t.Fatalf("%s %s changed %s: %q versus GET %q", method, path, header, value, getHeaders.Get(header))
						}
					}
					if res.Header.Get("Set-Cookie") != "" {
						t.Fatalf("%s %s issued a cookie", method, path)
					}
					if after := counts(); after != before {
						t.Fatalf("%s %s mutated person/session/challenge counts: before=%v after=%v", method, path, before, after)
					}
				}
			}
		})
	}
}
