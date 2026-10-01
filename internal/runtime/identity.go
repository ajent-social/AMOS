package runtime

import "net/http"

// IdentityHandlers is a startup composition seam, not business route registration.
// Nil fields retain explicit unavailable responses. Neither health/readiness nor
// arbitrary reserved prefixes can be replaced through this finite surface.
type IdentityHandlers struct {
	SignupPage  http.Handler
	SigninPage  http.Handler
	Signup      http.Handler
	Signin      http.Handler
	Signout     http.Handler
	VerifyEmail http.Handler
}

func (h IdentityHandlers) routes() map[string]http.Handler {
	return map[string]http.Handler{
		"GET /signup": h.SignupPage, "GET /signin": h.SigninPage,
		"POST /signup": h.Signup, "POST /auth": h.Signin, "POST /signout": h.Signout,
		"GET /verify-email": h.VerifyEmail, "HEAD /verify-email": h.VerifyEmail, "POST /verify-email": h.VerifyEmail,
	}
}
