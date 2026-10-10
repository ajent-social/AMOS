package mcpcontract

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

type protocolProfile struct {
	ProtocolVersion      string   `json:"protocolVersion"`
	Transport            string   `json:"transport"`
	Resource             string   `json:"resource"`
	Issuer               string   `json:"issuer"`
	Client               string   `json:"client"`
	EnabledCapabilities  []string `json:"enabledCapabilities"`
	DisabledCapabilities []string `json:"disabledCapabilities"`
	AMSLRevision         *string  `json:"amslRevision"`
}

var profileBlock = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")

func readProtocolProfile(t *testing.T) protocolProfile {
	t.Helper()
	raw, err := os.ReadFile("../../docs/contracts/mcp-profile.md")
	if err != nil {
		t.Fatal(err)
	}
	block := profileBlock.FindSubmatch(raw)
	if len(block) != 2 {
		t.Fatal("profile must contain one JSON fixture")
	}
	var p protocolProfile
	if err := json.Unmarshal(block[1], &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func validateProtocolProfile(p protocolProfile) error {
	if p.ProtocolVersion != "2026-07-28" {
		return fmt.Errorf("protocol version must be pinned to 2026-07-28")
	}
	if p.Transport != "streamable-http" || p.Resource != "https://amos.invalid/mcp" || p.Issuer != "https://identity.invalid/issuer" {
		return fmt.Errorf("transport, resource and issuer must match the bounded profile")
	}
	if p.Client != "registered-public-pkce" {
		return fmt.Errorf("client must be explicitly registered public PKCE")
	}
	if p.AMSLRevision != nil && strings.TrimSpace(*p.AMSLRevision) == "" {
		return fmt.Errorf("AMSL revision cannot be blank")
	}
	allowed := map[string]bool{"tools": true}
	seen := map[string]bool{}
	for _, capability := range p.EnabledCapabilities {
		if !allowed[capability] || seen[capability] {
			return fmt.Errorf("unsupported or duplicate enabled capability %q", capability)
		}
		seen[capability] = true
	}
	if !seen["tools"] {
		return fmt.Errorf("tools capability must be explicit")
	}
	for _, capability := range p.DisabledCapabilities {
		if seen[capability] {
			return fmt.Errorf("capability %q cannot be both enabled and disabled", capability)
		}
	}
	return nil
}

func TestProtocolProfile(t *testing.T) {
	base := readProtocolProfile(t)
	if err := validateProtocolProfile(base); err != nil {
		t.Fatalf("checked-in profile rejected: %v", err)
	}
	for name, mutate := range map[string]func(*protocolProfile){
		"missing protocol version": func(p *protocolProfile) { p.ProtocolVersion = "" },
		"unspecified client":       func(p *protocolProfile) { p.Client = "" },
		"unsupported capability": func(p *protocolProfile) {
			p.EnabledCapabilities = append(p.EnabledCapabilities, "client-metadata-fetch")
		},
		"unregistered capability": func(p *protocolProfile) { p.EnabledCapabilities = append(p.EnabledCapabilities, "sampling") },
		"wrong issuer":            func(p *protocolProfile) { p.Issuer = "https://attacker.invalid/issuer" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.EnabledCapabilities = append([]string(nil), base.EnabledCapabilities...)
			mutate(&candidate)
			if err := validateProtocolProfile(candidate); err == nil {
				t.Fatal("invalid profile was accepted")
			}
		})
	}
}
