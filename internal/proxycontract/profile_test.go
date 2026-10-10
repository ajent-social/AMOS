package proxycontract

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

type proxyProfile struct {
	Origin                string `json:"origin"`
	Reachability          string `json:"reachability"`
	TLS                   string `json:"tls"`
	TrustBootstrap        string `json:"trustBootstrap"`
	ClientIdentity        string `json:"clientIdentity"`
	KeyRotation           string `json:"keyRotation"`
	Issuer                string `json:"issuer"`
	Audience              string `json:"audience"`
	Identity              string `json:"identity"`
	MaxTokenSeconds       int    `json:"maxTokenSeconds"`
	TargetSource          string `json:"targetSource"`
	RequestTargetOverride bool   `json:"requestTargetOverride"`
	Redirects             string `json:"redirects"`
	Streaming             bool   `json:"streaming"`
	InternalRoutes        string `json:"internalRoutes"`
}

var profileBlock = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")

func readProxyProfile(t *testing.T) proxyProfile {
	t.Helper()
	raw, err := os.ReadFile("../../docs/contracts/business-proxy.md")
	if err != nil {
		t.Fatal(err)
	}
	block := profileBlock.FindSubmatch(raw)
	if len(block) != 2 {
		t.Fatal("proxy profile must contain one JSON fixture")
	}
	var p proxyProfile
	if err := json.Unmarshal(block[1], &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func validateProxyProfile(p proxyProfile) error {
	u, err := url.Parse(p.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() != "business.internal.invalid" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("origin must be the fixed HTTPS private profile origin")
	}
	if p.Reachability != "private-only" || p.TLS != "verified-mutual-tls" || p.TrustBootstrap != "deployment-pinned-private-ca-reference" || p.ClientIdentity != "deployment-bound-reference" {
		return fmt.Errorf("private reachability and authenticated TLS identity are required")
	}
	if p.KeyRotation != "key-id-with-24h-bounded-overlap" {
		return fmt.Errorf("identity key rotation must use the bounded key-ID overlap profile")
	}
	if p.Issuer != "https://amos.invalid/proxy-issuer" || p.Audience != "business-proxy-v1" || p.Identity != "server-signed-short-lived-person-and-workspace" || p.MaxTokenSeconds < 1 || p.MaxTokenSeconds > 60 {
		return fmt.Errorf("signed identity requires fixed issuer, audience and lifetime")
	}
	if p.TargetSource != "deployment-profile" || p.RequestTargetOverride {
		return fmt.Errorf("request-controlled upstream target is forbidden")
	}
	if p.Redirects != "reject" || p.Streaming || p.InternalRoutes != "deny" {
		return fmt.Errorf("redirects, streaming or internal routes violate the profile")
	}
	return nil
}

func TestProxyProfile(t *testing.T) {
	base := readProxyProfile(t)
	if err := validateProxyProfile(base); err != nil {
		t.Fatalf("checked-in profile rejected: %v", err)
	}
	for name, mutate := range map[string]func(*proxyProfile){
		"public unprotected origin": func(p *proxyProfile) {
			p.Origin = "http://business.example.com"
			p.TLS = "none"
			p.ClientIdentity = "none"
		},
		"arbitrary target URL": func(p *proxyProfile) {
			p.Origin = "https://attacker.invalid"
			p.TargetSource = "request"
			p.RequestTargetOverride = true
		},
		"missing audience binding":    func(p *proxyProfile) { p.Audience = "" },
		"unsigned client identity":    func(p *proxyProfile) { p.Identity = "client-provided-unsigned" },
		"unbounded identity lifetime": func(p *proxyProfile) { p.MaxTokenSeconds = 61 },
		"redirect following":          func(p *proxyProfile) { p.Redirects = "follow" },
		"internal route allowed":      func(p *proxyProfile) { p.InternalRoutes = "allow" },
	} {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if err := validateProxyProfile(candidate); err == nil {
				t.Fatal("unsafe profile was accepted")
			}
		})
	}
}
