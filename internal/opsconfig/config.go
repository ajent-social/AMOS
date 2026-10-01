// Package opsconfig defines owner supplied operational targets. Values in
// Defaults are proposals only; callers must explicitly select a profile and
// activate it before using them as installation targets.
package opsconfig

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Profile string

const (
	ProfileAWSManaged Profile = "aws_managed"
	ProfileAWSVM      Profile = "aws_vm"
)

var ErrInvalidConfig = errors.New("invalid operations configuration")

// Config separates proposed values from owner activation and measured evidence.
type Config struct {
	Profile         Profile
	Activated       bool
	AvailabilitySLO float64
	RPO             time.Duration
	RTO             time.Duration
	Retention       time.Duration
	BackupInterval  time.Duration
	CPUPercent      float64
	MemoryPercent   float64
	DiskPercent     float64
	AlertOwner      string
	RecoveryOwner   string
	IncidentOwner   string
}

// Defaults are deliberately conservative proposals. They confer no promise.
func Defaults() Config {
	return Config{
		AvailabilitySLO: 99.0,
		RPO:             24 * time.Hour,
		RTO:             72 * time.Hour,
		Retention:       30 * 24 * time.Hour,
		BackupInterval:  24 * time.Hour,
		CPUPercent:      80,
		MemoryPercent:   80,
		DiskPercent:     80,
	}
}

// Validate rejects unset targets, invalid profile selection, and recovery
// schedules that cannot meet the declared data-loss target.
func (c Config) Validate() error {
	if c.Profile != ProfileAWSManaged && c.Profile != ProfileAWSVM {
		return fmt.Errorf("%w: profile must be explicitly selected", ErrInvalidConfig)
	}
	if math.IsNaN(c.AvailabilitySLO) || math.IsInf(c.AvailabilitySLO, 0) || c.AvailabilitySLO <= 0 || c.AvailabilitySLO >= 100 || c.RPO <= 0 || c.RTO <= 0 || c.Retention <= 0 || c.BackupInterval <= 0 {
		return fmt.Errorf("%w: SLO, RPO, RTO, retention, and backup interval must be positive; SLO must be below 100", ErrInvalidConfig)
	}
	if c.Retention < c.RPO {
		return fmt.Errorf("%w: retention must cover at least the declared RPO", ErrInvalidConfig)
	}
	if c.BackupInterval > c.RPO {
		return fmt.Errorf("%w: backup interval exceeds declared RPO", ErrInvalidConfig)
	}
	if !threshold(c.CPUPercent) || !threshold(c.MemoryPercent) || !threshold(c.DiskPercent) {
		return fmt.Errorf("%w: resource thresholds must be greater than zero and at most 100", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.AlertOwner) == "" || strings.TrimSpace(c.RecoveryOwner) == "" || strings.TrimSpace(c.IncidentOwner) == "" {
		return fmt.Errorf("%w: alert, recovery, and incident owners must be named", ErrInvalidConfig)
	}
	return nil
}

func threshold(value float64) bool { return value > 0 && value <= 100 }

// Evidence describes an observed result, kept distinct from target
// configuration. It is a schema only; this package has no trusted measurement
// source and must not treat caller-supplied fields as verified evidence.
type Evidence struct {
	Profile    Profile `json:"profile"`
	MeasuredAt string  `json:"measured_at"`
	Result     string  `json:"result"`
	Source     string  `json:"source"`
	Scope      string  `json:"scope"`
}
