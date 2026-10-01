package email

import "testing"

func TestExplicitDevelopmentEmailOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin     string
		dev, valid bool
	}{{"https://app.example.test", false, true}, {"http://app.example.test", false, false}, {"http://app.example.test", true, false}, {"https://app.example.test", true, false}, {"http://127.0.0.1:8080", true, true}, {"http://localhost:8080", true, true}, {"http://[::1]:8080", true, true}, {"http://127.0.0.1:8080", false, false},
		{"http://127.0.0.1:0", true, false}, {"http://127.0.0.2:8080", true, false}, {"http://localhost.evil.example:8080", true, false}, {"http://user@localhost:8080", true, false}, {"http://localhost:8080/path", true, false}} {
		_, e := ParseApplicationOrigin(tc.origin, tc.dev)
		if (e == nil) != tc.valid {
			t.Errorf("origin=%q dev=%v error=%v", tc.origin, tc.dev, e)
		}
	}
	r, e := NewRenderer(RenderConfig{FromAddress: "amos@example.test", ApplicationOrigin: "http://127.0.0.1:8080", DevelopmentLoopback: true, MaxBodyBytes: MaxBodyBytes})
	if e != nil {
		t.Fatal(e)
	}
	if !r.allowedActionURL("http://127.0.0.1:8080/verify-email?token=synthetic") || r.allowedActionURL("https://evil.example.test/verify-email") {
		t.Fatal("action origin mismatch")
	}
}
