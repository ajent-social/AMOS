package opsconfig

import (
	"math"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	c := Defaults()
	c.Profile = ProfileAWSManaged
	c.AlertOwner, c.RecoveryOwner, c.IncidentOwner = "installation owner", "installation owner", "installation owner"
	return c
}

func TestConfigDefaultsAreProposedAndRequireProfileAndOwners(t *testing.T) {
	c := Defaults()
	if c.Activated {
		t.Fatal("proposed defaults unexpectedly activated")
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("unset profile accepted: %v", err)
	}
	c.Profile = ProfileAWSManaged
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "owners") {
		t.Fatalf("unset owners accepted: %v", err)
	}
}

func TestConfigRejectsUnsetInvalidAndImpossibleRecoveryValues(t *testing.T) {
	base := validConfig()
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"invalid profile", func(c *Config) { c.Profile = "unknown" }},
		{"unset RPO", func(c *Config) { c.RPO = 0 }},
		{"unset RTO", func(c *Config) { c.RTO = 0 }},
		{"unset retention", func(c *Config) { c.Retention = 0 }},
		{"invalid retention", func(c *Config) { c.Retention = -time.Hour }},
		{"retention shorter than RPO", func(c *Config) { c.Retention = time.Hour }},
		{"backup interval greater than RPO", func(c *Config) { c.BackupInterval = c.RPO + time.Second }},
		{"invalid SLO", func(c *Config) { c.AvailabilitySLO = 100 }},
		{"NaN SLO", func(c *Config) { c.AvailabilitySLO = math.NaN() }},
		{"positive infinite SLO", func(c *Config) { c.AvailabilitySLO = math.Inf(1) }},
		{"negative infinite SLO", func(c *Config) { c.AvailabilitySLO = math.Inf(-1) }},
		{"whitespace alert owner", func(c *Config) { c.AlertOwner = " \t\n " }},
		{"whitespace recovery owner", func(c *Config) { c.RecoveryOwner = " \t\n " }},
		{"whitespace incident owner", func(c *Config) { c.IncidentOwner = " \t\n " }},
		{"invalid threshold", func(c *Config) { c.CPUPercent = 101 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.edit(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestConfigDefaultsDoNotActivateTargets(t *testing.T) {
	if Defaults().Activated {
		t.Fatal("proposed defaults unexpectedly activated")
	}
}

func TestConfigAcceptsBackupIntervalEqualToRPO(t *testing.T) {
	c := validConfig()
	c.BackupInterval = c.RPO
	if err := c.Validate(); err != nil {
		t.Fatalf("configuration with backup interval equal to RPO rejected: %v", err)
	}
}
