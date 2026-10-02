package runtime

import "net/http"

// IdentityHandlers is a startup composition seam, not business route registration.
// Nil fields retain explicit unavailable responses. Neither health/readiness nor
// arbitrary reserved prefixes can be replaced through this finite surface.
type IdentityHandlers struct {
	FederationCallback                                 http.Handler
	SignupPage                                         http.Handler
	SigninPage                                         http.Handler
	Signup                                             http.Handler
	Signin                                             http.Handler
	Signout                                            http.Handler
	VerifyEmail                                        http.Handler
	ForgotPassword                                     http.Handler
	ResetPassword                                      http.Handler
	TOTPStatus, TOTPEnroll, TOTPConfirm, TOTPChallenge http.Handler
}

func (h IdentityHandlers) routes() map[string]http.Handler {
	return map[string]http.Handler{
		"GET /oauth/callback":              h.FederationCallback,
		"GET /account/mfa/totp":            h.TOTPStatus,
		"POST /account/mfa/totp/enroll":    h.TOTPEnroll,
		"POST /account/mfa/totp/confirm":   h.TOTPConfirm,
		"POST /account/mfa/totp/challenge": h.TOTPChallenge,
		"GET /signup":                      h.SignupPage, "GET /signin": h.SigninPage,
		"POST /signup": h.Signup, "POST /auth": h.Signin, "POST /signout": h.Signout,
		"GET /verify-email": h.VerifyEmail, "HEAD /verify-email": h.VerifyEmail, "POST /verify-email": h.VerifyEmail,
		"GET /forgot-password": h.ForgotPassword, "HEAD /forgot-password": h.ForgotPassword, "POST /forgot-password": h.ForgotPassword,
		"GET /reset-password": h.ResetPassword, "HEAD /reset-password": h.ResetPassword, "POST /reset-password": h.ResetPassword,
	}
}
