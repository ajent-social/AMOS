package email

import "testing"

func TestDevelopmentTLSOriginExactPrivateIPv4Grammar(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:443", "https://10.0.0.1:8443", "https://172.16.0.1:8443", "https://172.31.255.254:8443", "https://192.168.0.1:65535"} {
		u, err := ParseDevelopmentTLSOrigin(origin)
		if err != nil || u.String() != origin {
			t.Fatalf("synthetic development origin rejected: %v", err)
		}
	}
	for _, origin := range []string{"http://127.0.0.1:8443", "https://127.0.0.1", "https://127.0.0.1:0", "https://127.0.0.1:08443", "https://127.0.0.1:+8443", "https://127.0.0.1:65536", "https://127.0.0.1:https", "https://127.0.0.1:8443/", "https://127.0.0.1:8443?", "https://127.0.0.1:8443#x", "https://127.0.0.1:8443?x=y", "https://user@127.0.0.1:8443", "HTTPS://127.0.0.1:8443", "https://127.0.0.2:8443", "https://0.0.0.0:8443", "https://192.0.2.1:8443", "https://172.15.255.255:8443", "https://172.32.0.1:8443", "https://169.254.1.1:8443", "https://224.0.0.1:8443", "https://localhost:8443", "https://app.example.invalid:8443", "https://[::1]:8443", "https://[::ffff:127.0.0.1]:8443", "https://127.000.000.001:8443", "https://2130706433:8443", " https://127.0.0.1:8443", "https://127.0.0.1:8443\n"} {
		if _, err := ParseDevelopmentTLSOrigin(origin); err == nil {
			t.Fatal("noncanonical or unauthorized origin accepted")
		}
	}
	// The old grant remains explicitly loopback-only.
	if _, err := ParseApplicationOrigin("https://10.0.0.1:8443", true); err == nil {
		t.Fatal("legacy development origin widened")
	}
}
