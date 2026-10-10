package cloudflare

import (
	"errors"
	"testing"
)

func validMode() Mode {
	return Mode{
		Profile:  ProfileAWSManaged,
		ZoneID:   "zone-123",
		ZoneName: "example.com",
		Hostname: "app.example.com",
		Proxy:    ProxyDNSOnly,
	}
}

func TestModeDNSOnlyReportsNoProxyProtections(t *testing.T) {
	for _, profile := range []HostingProfile{ProfileAWSManaged, ProfileAWSVM} {
		mode := validMode()
		mode.Profile = profile
		if err := mode.Validate(); err != nil {
			t.Fatalf("%s: Validate() error = %v", profile, err)
		}
		if got := mode.Capabilities(); got != (Capabilities{DNS: true}) {
			t.Fatalf("%s: Capabilities() = %+v, want DNS only", profile, got)
		}
	}
}

func TestModeRejectsProxiedConfiguration(t *testing.T) {
	mode := validMode()
	mode.Proxy = ProxyEnabled
	if err := mode.Validate(); !errors.Is(err, ErrProxyUnsupported) {
		t.Fatalf("Validate() error = %v, want ErrProxyUnsupported", err)
	}
}

func TestModeRejectsIncompleteAndOutOfZoneConfiguration(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Mode)
	}{
		{"missing zone", func(m *Mode) { m.ZoneID = " " }},
		{"padded zone ID", func(m *Mode) { m.ZoneID = " zone-123 " }},
		{"missing proxy mode", func(m *Mode) { m.Proxy = "" }},
		{"hostname outside zone", func(m *Mode) { m.Hostname = "app.other.test" }},
		{"unsupported profile", func(m *Mode) { m.Profile = "aws_other" }},
		{"invalid hostname", func(m *Mode) { m.Hostname = "app:443.example.com" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode := validMode()
			tc.edit(&mode)
			if err := mode.Validate(); err == nil {
				t.Fatal("Validate() succeeded for invalid mode")
			}
		})
	}
}

func TestModeAliasOptionsRestrictZoneHostnameAndProxy(t *testing.T) {
	mode := validMode()
	got, err := mode.AliasOptions(AliasRequest{
		ZoneID:   "zone-123",
		Hostname: "APP.EXAMPLE.COM.",
		Proxy:    ProxyDNSOnly,
	})
	if err != nil {
		t.Fatalf("AliasOptions() error = %v", err)
	}
	if got != (AliasOptions{ZoneID: "zone-123", Hostname: "app.example.com", Proxied: false}) {
		t.Fatalf("AliasOptions() = %+v", got)
	}

	cases := []struct {
		name string
		edit func(*AliasRequest)
		want error
	}{
		{"foreign zone", func(r *AliasRequest) { r.ZoneID = "other-zone" }, nil},
		{"foreign hostname", func(r *AliasRequest) { r.Hostname = "other.example.com" }, nil},
		{"proxied", func(r *AliasRequest) { r.Proxy = ProxyEnabled }, ErrProxyUnsupported},
		{"implicit mode", func(r *AliasRequest) { r.Proxy = "" }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := AliasRequest{ZoneID: "zone-123", Hostname: "app.example.com", Proxy: ProxyDNSOnly}
			tc.edit(&req)
			_, err := mode.AliasOptions(req)
			if err == nil {
				t.Fatal("AliasOptions() succeeded for invalid request")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("AliasOptions() error = %v, want %v", err, tc.want)
			}
		})
	}
}
