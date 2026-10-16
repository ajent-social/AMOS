// Package upstreamconfig validates a declared upstream deployment design. It
// neither configures AWS nor proves effective IAM, storage or network isolation.
package upstreamconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Schema = "amos-upstream-config-v1"

var ErrInvalid = errors.New("upstream configuration rejected")
var accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
var regionPattern = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[1-9]$`)
var bucketPattern = regexp.MustCompile(`^amos-upstream-[a-z0-9][a-z0-9-]{2,38}$`)
var kmsPattern = regexp.MustCompile(`^arn:aws:kms:([a-z]{2}-[a-z]+-[1-9]):([0-9]{12}):key/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var rolePattern = regexp.MustCompile(`^arn:aws:iam::([0-9]{12}):role/amos-(upstream|consumer)/(operator|intake|storage|deploy)/[a-z][a-z0-9-]{0,31}$`)

type Config struct {
	Schema     string       `json:"schema"`
	Profile    string       `json:"profile"`
	Upstream   AccountScope `json:"upstream"`
	Consumers  []Consumer   `json:"consumers"`
	Identities Identities   `json:"identities"`
	Storage    Storage      `json:"storage"`
	Collection Collection   `json:"collection"`
	Abuse      Abuse        `json:"abuse"`
}
type AccountScope struct {
	Region            string `json:"region"`
	AccountID         string `json:"account_id"`
	Domain            string `json:"domain"`
	IdentityNamespace string `json:"identity_namespace"`
}
type Consumer struct {
	AccountID     string `json:"account_id"`
	Domain        string `json:"domain"`
	DeploymentARN string `json:"deployment_arn"`
}
type Role struct {
	ARN       string `json:"arn"`
	AccountID string `json:"account_id"`
	Domain    string `json:"domain"`
	Purpose   string `json:"purpose"`
}
type Identities struct {
	Operator   Role `json:"operator"`
	Intake     Role `json:"intake"`
	Storage    Role `json:"storage"`
	Deployment Role `json:"deployment"`
}
type Storage struct {
	BucketName       string `json:"bucket_name"`
	KMSKeyARN        string `json:"kms_key_arn"`
	AccountID        string `json:"account_id"`
	Namespace        string `json:"namespace"`
	Access           string `json:"access"`
	RecordFormat     string `json:"record_format"`
	RawPayloadPolicy string `json:"raw_payload_policy"`
	Encryption       string `json:"encryption"`
	RetentionHours   int    `json:"retention_hours"`
	MaxRecords       int    `json:"max_records"`
	MaxBytes         int    `json:"max_bytes"`
}
type SenderBudget struct {
	PerPrincipal24H int `json:"per_principal_24h"`
	Global24H       int `json:"global_24h"`
	MaxPrincipals   int `json:"max_principals"`
}
type Collection struct {
	Mode               string       `json:"mode"`
	DiagnosticProfile  string       `json:"diagnostic_profile"`
	MaxPayloadBytes    int          `json:"max_payload_bytes"`
	MaxCompressedBytes int          `json:"max_compressed_bytes"`
	TimeoutSeconds     int          `json:"timeout_seconds"`
	MaxConcurrent      int          `json:"max_concurrent"`
	DailyReports       int          `json:"daily_reports"`
	Anonymous          SenderBudget `json:"anonymous"`
	Enrolled           SenderBudget `json:"enrolled"`
}
type Reference struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
}
type Abuse struct {
	KeyReference   Reference `json:"key_reference"`
	SourceKeyMode  string    `json:"source_key_mode"`
	ClockPolicy    string    `json:"clock_policy"`
	InvalidContent string    `json:"invalid_content"`
}

// DefaultCollection returns only requested limits. It never selects identities,
// accounts, resources or credentials. A complete explicit config remains needed.
func DefaultCollection() Collection {
	return Collection{Mode: "disabled", DiagnosticProfile: "amos-public-diagnostic-v1", MaxPayloadBytes: 8192, MaxCompressedBytes: 8192, TimeoutSeconds: 5, MaxConcurrent: 2, DailyReports: 32, Anonymous: SenderBudget{1, 8, 128}, Enrolled: SenderBudget{1, 24, 1024}}
}

// Decode rejects unknown, duplicate, missing and case-aliased keys recursively.
// The reflect walk is an exact struct-key check, not a JSON Schema interpreter.
func Decode(data []byte) (Config, error) {
	if len(data) == 0 || len(data) > 32768 || !utf8.Valid(data) {
		return Config{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	nodes := 0
	value, err := readValue(d, 0, &nodes)
	if err != nil {
		return Config{}, ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF || !exactShape(value, reflect.TypeFor[Config]()) {
		return Config{}, ErrInvalid
	}
	var c Config
	if json.Unmarshal(data, &c) != nil || Validate(c) != nil {
		return Config{}, ErrInvalid
	}
	return c, nil
}
func Validate(c Config) error {
	if c.Schema != Schema || (c.Profile != "aws_managed" && c.Profile != "aws_vm") || !accountPattern.MatchString(c.Upstream.AccountID) || !regionPattern.MatchString(c.Upstream.Region) || c.Upstream.Domain != "upstream" || c.Upstream.IdentityNamespace != "amos-upstream-operators" || len(c.Consumers) < 1 || len(c.Consumers) > 64 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, consumer := range c.Consumers {
		if consumer.Domain != "consumer" || consumer.AccountID == c.Upstream.AccountID || seen[consumer.AccountID] || !role(consumer.DeploymentARN, consumer.AccountID, "consumer", "deploy") {
			return ErrInvalid
		}
		seen[consumer.AccountID] = true
	}
	arns := map[string]bool{}
	for purpose, r := range map[string]Role{"operator": c.Identities.Operator, "intake": c.Identities.Intake, "storage": c.Identities.Storage, "deploy": c.Identities.Deployment} {
		if r.Domain != "upstream" || r.Purpose != purpose || r.AccountID != c.Upstream.AccountID || !role(r.ARN, r.AccountID, "upstream", purpose) || arns[r.ARN] {
			return ErrInvalid
		}
		arns[r.ARN] = true
	}
	s := c.Storage
	kms := kmsPattern.FindStringSubmatch(s.KMSKeyARN)
	if len(kms) != 3 || kms[1] != c.Upstream.Region || kms[2] != c.Upstream.AccountID || !bucketPattern.MatchString(s.BucketName) {
		return ErrInvalid
	}
	if s.AccountID != c.Upstream.AccountID || s.Namespace != "amos-upstream-diagnostics" || s.Access != "operator-only" || s.RecordFormat != "normalized-quarantine-v1" || s.RawPayloadPolicy != "discard" || s.Encryption != "aws:kms" || !within(s.RetentionHours, 1, 168) || !within(s.MaxRecords, 1, 1024) || !within(s.MaxBytes, 8192, 8388608) {
		return ErrInvalid
	}
	p := c.Collection
	if (p.Mode != "disabled" && p.Mode != "enabled") || p.DiagnosticProfile != "amos-public-diagnostic-v1" || !within(p.MaxPayloadBytes, 1, 8192) || !within(p.MaxCompressedBytes, 1, 8192) || !within(p.TimeoutSeconds, 1, 5) || !within(p.MaxConcurrent, 1, 4) || !within(p.DailyReports, 1, 64) || !sender(p.Anonymous, 16, 256) || !sender(p.Enrolled, 64, 1024) || p.Anonymous.Global24H+p.Enrolled.Global24H > p.DailyReports {
		return ErrInvalid
	}
	a := c.Abuse
	if a.SourceKeyMode != "daily-hmac" || a.ClockPolicy != "deny-on-unknown" || a.InvalidContent != "discard-before-storage" || a.KeyReference.Provider != "aws_secrets_manager" || a.KeyReference.AccountID != c.Upstream.AccountID || a.KeyReference.Name != "amos-upstream/abuse-key" {
		return ErrInvalid
	}
	return nil
}
func role(arn, account, domain, purpose string) bool {
	m := rolePattern.FindStringSubmatch(arn)
	return len(m) == 4 && accountPattern.MatchString(account) && m[1] == account && m[2] == domain && m[3] == purpose
}
func within(v, low, high int) bool { return v >= low && v <= high }
func sender(b SenderBudget, maxReports, maxPrincipals int) bool {
	return within(b.PerPrincipal24H, 0, 1) && within(b.Global24H, 0, maxReports) && within(b.MaxPrincipals, 1, maxPrincipals) && ((b.PerPrincipal24H == 0) == (b.Global24H == 0))
}

func exactShape(v any, t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct:
		object, ok := v.(map[string]any)
		if !ok || len(object) != t.NumField() {
			return false
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := f.Tag.Get("json")
			child, exists := object[name]
			if !exists || !exactShape(child, f.Type) {
				return false
			}
		}
		return true
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return false
		}
		for _, child := range a {
			if !exactShape(child, t.Elem()) {
				return false
			}
		}
		return true
	case reflect.String:
		_, ok := v.(string)
		return ok
	case reflect.Int:
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := strconv.ParseInt(string(n), 10, 64)
		return err == nil
	}
	return false
}
func readValue(d *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 8 || *nodes > 2048 {
		return nil, ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	if s, ok := token.(string); ok && len(s) > 512 {
		return nil, ErrInvalid
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for d.More() {
			if len(object) >= 32 {
				return nil, ErrInvalid
			}
			key, err := d.Token()
			if err != nil {
				return nil, ErrInvalid
			}
			name, ok := key.(string)
			if !ok || len(name) > 128 || strings.ContainsRune(name, 0) {
				return nil, ErrInvalid
			}
			if _, exists := object[name]; exists {
				return nil, ErrInvalid
			}
			child, err := readValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			object[name] = child
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return object, nil
	case '[':
		array := []any{}
		for d.More() {
			if len(array) >= 64 {
				return nil, ErrInvalid
			}
			child, err := readValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			array = append(array, child)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrInvalid
		}
		return array, nil
	}
	return nil, ErrInvalid
}
