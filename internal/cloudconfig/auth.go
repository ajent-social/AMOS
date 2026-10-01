package cloudconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"go.yaml.in/yaml/v4"
)

const maxAuthDocumentBytes = 64 << 10

var (
	ErrInvalidAuthentication = errors.New("invalid cloud authentication configuration")
	profileNamePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	envNamePattern           = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	accountIDPattern         = regexp.MustCompile(`^[0-9]{12}$`)
	regionPattern            = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]$`)
	zoneIDPattern            = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// AuthenticationDocument contains references and role identities only. It has
// no fields capable of carrying resolved AWS credentials or runtime app secrets.
type AuthenticationDocument struct {
	Version     int                `yaml:"version"`
	Environment string             `yaml:"environment"`
	AWS         AWSIdentity        `yaml:"aws"`
	Roles       RoleReferences     `yaml:"roles"`
	Cloudflare  CloudflareIdentity `yaml:"cloudflare"`
	PulumiState PulumiState        `yaml:"pulumiState"`
}

type AWSIdentity struct {
	AccountID      string        `yaml:"accountId"`
	Region         string        `yaml:"region"`
	CredentialRole string        `yaml:"credentialRole"`
	Profile        string        `yaml:"profile,omitempty"`
	OIDC           *OIDCIdentity `yaml:"oidc,omitempty"`
}

type OIDCIdentity struct {
	TokenFileEnv string `yaml:"tokenFileEnv"`
	Audience     string `yaml:"audience"`
}

type RoleReferences struct {
	Bootstrap string `yaml:"bootstrap"`
	Preview   string `yaml:"preview"`
	Deploy    string `yaml:"deploy"`
	Host      string `yaml:"host"`
	Recovery  string `yaml:"recovery"`
}

type CloudflareIdentity struct {
	ZoneID      string   `yaml:"zoneId"`
	TokenEnv    string   `yaml:"tokenEnv"`
	Permissions []string `yaml:"permissions"`
}

type PulumiState struct {
	EncryptionMode  string `yaml:"encryptionMode"`
	KMSKeyARN       string `yaml:"kmsKeyArn,omitempty"`
	RecoveryRoleARN string `yaml:"recoveryRoleArn,omitempty"`
	PassphraseEnv   string `yaml:"passphraseEnv,omitempty"`
}

// DecodeAuthentication parses a bounded YAML/JSON document, rejects unknown
// fields, then validates reference formats and environment separation.
func DecodeAuthentication(r io.Reader) (AuthenticationDocument, error) {
	var doc AuthenticationDocument
	if r == nil {
		return doc, ErrInvalidAuthentication
	}
	data, err := io.ReadAll(io.LimitReader(r, maxAuthDocumentBytes+1))
	if err != nil {
		return doc, fmt.Errorf("read cloud authentication document: %w", err)
	}
	if len(data) == 0 || len(data) > maxAuthDocumentBytes {
		return doc, ErrInvalidAuthentication
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&doc); err != nil {
		return doc, fmt.Errorf("decode cloud authentication document: %w", ErrInvalidAuthentication)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return doc, ErrInvalidAuthentication
	}
	if err := doc.Validate(); err != nil {
		return doc, err
	}
	return doc, nil
}

func (d AuthenticationDocument) Validate() error {
	if d.Version != 1 || !validEnvironment(d.Environment) || !accountIDPattern.MatchString(d.AWS.AccountID) || !validRegion(d.AWS.Region) {
		return ErrInvalidAuthentication
	}
	if (d.AWS.Profile == "") == (d.AWS.OIDC == nil) || !oneOf(d.AWS.CredentialRole, "bootstrap", "preview", "deploy") {
		return ErrInvalidAuthentication
	}
	if d.AWS.Profile != "" && d.AWS.CredentialRole == "deploy" || d.AWS.OIDC != nil && d.AWS.CredentialRole == "bootstrap" {
		return ErrInvalidAuthentication
	}
	if d.AWS.Profile != "" && !profileNamePattern.MatchString(d.AWS.Profile) {
		return ErrInvalidAuthentication
	}
	if d.AWS.OIDC != nil && (!validEnvReference(d.AWS.OIDC.TokenFileEnv) || !validAudience(d.AWS.OIDC.Audience)) {
		return ErrInvalidAuthentication
	}
	arns := []string{d.Roles.Bootstrap, d.Roles.Preview, d.Roles.Deploy, d.Roles.Host, d.Roles.Recovery}
	seen := make(map[string]struct{}, len(arns))
	for _, arn := range arns {
		if !validRoleARN(arn, d.AWS.AccountID) || !strings.HasSuffix(roleName(arn), "-"+d.Environment) {
			return ErrInvalidAuthentication
		}
		if _, ok := seen[arn]; ok {
			return ErrInvalidAuthentication
		}
		seen[arn] = struct{}{}
	}
	if !zoneIDPattern.MatchString(d.Cloudflare.ZoneID) || !validEnvReference(d.Cloudflare.TokenEnv) || strings.TrimPrefix(d.Cloudflare.TokenEnv, "env://") != "AMOS_CF_"+strings.ToUpper(d.Environment)+"_DNS_TOKEN" || !sameStrings(d.Cloudflare.Permissions, []string{"dns:edit", "zone:read"}) {
		return ErrInvalidAuthentication
	}
	switch d.PulumiState.EncryptionMode {
	case "kms":
		if !validKMSARN(d.PulumiState.KMSKeyARN, d.AWS.AccountID, d.AWS.Region) || !validRoleARN(d.PulumiState.RecoveryRoleARN, d.AWS.AccountID) || !strings.HasSuffix(roleName(d.PulumiState.RecoveryRoleARN), "-"+d.Environment) || d.PulumiState.PassphraseEnv != "" || d.PulumiState.RecoveryRoleARN != d.Roles.Recovery {
			return ErrInvalidAuthentication
		}
	case "local-passphrase":
		if d.Environment != "development" || d.PulumiState.PassphraseEnv == "" || !validEnvReference(d.PulumiState.PassphraseEnv) || d.PulumiState.KMSKeyARN != "" || d.PulumiState.RecoveryRoleARN != "" {
			return ErrInvalidAuthentication
		}
	default:
		return ErrInvalidAuthentication
	}
	return nil
}

// LoadAWSConfig resolves only the configured local profile or web identity.
func (d AuthenticationDocument) LoadAWSConfig(ctx context.Context) (aws.Config, error) {
	return d.loadAWSConfig(ctx, os.LookupEnv)
}

func (d AuthenticationDocument) loadAWSConfig(ctx context.Context, lookupEnv func(string) (string, bool)) (aws.Config, error) {
	if ctx == nil || d.Validate() != nil {
		return aws.Config{}, ErrInvalidAuthentication
	}
	if lookupEnv == nil {
		return aws.Config{}, ErrInvalidAuthentication
	}
	for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
		if value, ok := lookupEnv(key); ok && value != "" {
			return aws.Config{}, ErrInvalidAuthentication
		}
	}
	if d.AWS.Profile != "" {
		if d.Environment == "production" {
			return aws.Config{}, ErrInvalidAuthentication
		}
		profile, err := awsconfig.LoadSharedConfigProfile(ctx, d.AWS.Profile, func(o *awsconfig.LoadSharedConfigOptions) {
			o.ConfigFiles = sharedConfigFiles(lookupEnv)
			o.CredentialsFiles = sharedCredentialsFiles(lookupEnv)
		})
		if err != nil {
			return aws.Config{}, fmt.Errorf("load selected AWS profile: %w", err)
		}
		if profileHasStaticCredentials(profile) {
			return aws.Config{}, ErrInvalidAuthentication
		}
		loaded, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithSharedConfigProfile(d.AWS.Profile), awsconfig.WithRegion(d.AWS.Region))
		if err != nil {
			return aws.Config{}, fmt.Errorf("load selected AWS SDK profile: %w", err)
		}
		loaded.Credentials = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(loaded), d.selectedRoleARN(), func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "amos-" + d.AWS.CredentialRole + "-" + d.Environment
		})
		return loaded, nil
	}
	tokenPath, ok := lookupEnv(strings.TrimPrefix(d.AWS.OIDC.TokenFileEnv, "env://"))
	if !ok || tokenPath == "" || !strings.HasPrefix(tokenPath, "/") {
		return aws.Config{}, ErrInvalidAuthentication
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(d.AWS.Region))
	if err != nil {
		return aws.Config{}, fmt.Errorf("load AWS SDK configuration: %w", err)
	}
	loaded.Credentials = stscreds.NewWebIdentityRoleProvider(sts.NewFromConfig(loaded), d.selectedRoleARN(), stscreds.IdentityTokenFile(tokenPath), func(o *stscreds.WebIdentityRoleOptions) {
		o.RoleSessionName = "amos-" + d.AWS.CredentialRole + "-" + d.Environment
	})
	return loaded, nil
}

func (d AuthenticationDocument) selectedRoleARN() string {
	switch d.AWS.CredentialRole {
	case "bootstrap":
		return d.Roles.Bootstrap
	case "preview":
		return d.Roles.Preview
	case "deploy":
		return d.Roles.Deploy
	default:
		return ""
	}
}

func sharedConfigFiles(lookupEnv func(string) (string, bool)) []string {
	if path, ok := lookupEnv("AWS_CONFIG_FILE"); ok && path != "" {
		return []string{path}
	}
	return awsconfig.DefaultSharedConfigFiles
}

func sharedCredentialsFiles(lookupEnv func(string) (string, bool)) []string {
	if path, ok := lookupEnv("AWS_SHARED_CREDENTIALS_FILE"); ok && path != "" {
		return []string{path}
	}
	return awsconfig.DefaultSharedCredentialsFiles
}

func profileHasStaticCredentials(profile awsconfig.SharedConfig) bool {
	if profile.Credentials.AccessKeyID != "" || profile.Credentials.SecretAccessKey != "" || profile.Credentials.SessionToken != "" {
		return true
	}
	return profile.Source != nil && profileHasStaticCredentials(*profile.Source)
}

func validEnvironment(s string) bool {
	return s == "development" || s == "staging" || s == "production"
}
func oneOf(s string, options ...string) bool {
	for _, option := range options {
		if s == option {
			return true
		}
	}
	return false
}
func validRegion(s string) bool { return regionPattern.MatchString(s) }
func validAudience(s string) bool {
	return len(s) >= 1 && len(s) <= 256 && !strings.ContainsAny(s, "\r\n")
}
func validEnvReference(s string) bool {
	return strings.HasPrefix(s, "env://") && envNamePattern.MatchString(strings.TrimPrefix(s, "env://"))
}

func roleName(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) != 6 {
		return ""
	}
	return parts[5][len("role/"):]
}

func validRoleARN(arn, account string) bool {
	parts := strings.Split(arn, ":")
	return len(parts) == 6 && parts[0] == "arn" && parts[1] == "aws" && parts[2] == "iam" && parts[3] == "" && parts[4] == account && strings.HasPrefix(parts[5], "role/") && len(strings.TrimPrefix(parts[5], "role/")) > 0 && !strings.Contains(parts[5], "*")
}

func validKMSARN(arn, account, region string) bool {
	parts := strings.Split(arn, ":")
	return len(parts) == 6 && parts[0] == "arn" && parts[1] == "aws" && parts[2] == "kms" && parts[3] == region && parts[4] == account && strings.HasPrefix(parts[5], "key/") && len(strings.TrimPrefix(parts[5], "key/")) > 0 && !strings.Contains(parts[5], "*")
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]struct{}, len(got))
	for _, item := range got {
		seen[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := seen[item]; !ok {
			return false
		}
	}
	return true
}
