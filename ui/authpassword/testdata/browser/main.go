// Presentation-only browser fixture. Synthetic backend statuses do not qualify
// credential verification, sessions, email delivery or PostgreSQL behavior.
package main

import (
	"context"
	identityemail "github.com/ajent-social/amos/identity/email"
	"github.com/ajent-social/amos/ui/authpassword"
	"github.com/google/uuid"
	"log"
	"net/http"
	"time"
)

type verification struct{}

func (verification) Preview(context.Context, uuid.UUID, string) (identityemail.Page, error) {
	return identityemail.Page{Available: true}, nil
}

type reset struct{}

func (reset) Preview(context.Context, uuid.UUID, string) (bool, error) { return true, nil }
func status(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
}
func main() {
	p, err := authpassword.New(authpassword.Config{Theme: &authpassword.Theme{BrandName: `Example <script>unsafe</script>`, LogoURL: "/assets/logo.svg", StylesheetURL: "/assets/auth.css"}, Signup: status(202), Signin: status(401), RecoveryRequest: status(202), RecoveryComplete: status(204), VerificationConfirm: status(204), Verification: verification{}, Reset: reset{}, AllowsOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "http://127.0.0.1:18674" }})
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", p.Handler())
	mux.HandleFunc("/assets/auth.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(`*{box-sizing:border-box}body{margin:0;background:#f7f3ee;color:#261b15;font-family:sans-serif}.amos-auth-brand,.amos-auth-main{max-width:440px;margin:24px auto;padding:16px}.amos-auth-brand a{display:flex;align-items:center;gap:12px;color:inherit;text-decoration:none}.amos-auth-form{display:grid;gap:12px}input,button{font:inherit;padding:12px;width:100%}input:focus,button:focus,a:focus{outline:3px solid #287b6c;outline-offset:3px}button{background:#287b6c;color:white;border:0;border-radius:8px}.amos-auth-message{border-left:3px solid #287b6c;padding:12px}.amos-auth-links a{color:#287b6c}`))
	})
	mux.HandleFunc("/assets/logo.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" rx="8" fill="#287b6c"/></svg>`))
	})
	srv := &http.Server{Addr: "127.0.0.1:18674", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { time.Sleep(300 * time.Second); srv.Shutdown(context.Background()) }()
	log.Print("presentation-only browser fixture listening")
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
