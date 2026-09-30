// Package ses sends AMOS email through Amazon SES API v2.
package ses

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/ajent-social/amos/delivery/email"
	"github.com/ajent-social/amos/delivery/email/internal/receipt"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/google/uuid"
)

const (
	DefaultRequestTimeout = 15 * time.Second
	ProviderRetryDelay    = 30 * time.Second
	JobTagName            = "amos-job-id"
)

type Config struct {
	Region            string
	CredentialRef     string
	FromAddress       string
	ApplicationOrigin string
	ConfigurationSet  string
	RequestTimeout    time.Duration
}

type CredentialResolver interface {
	ResolveAWS(context.Context, string) (aws.CredentialsProvider, error)
}

type EventSource = email.VerifiedEventSource

type client interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

type Sender struct {
	client           client
	events           EventSource
	from             string
	configurationSet string
	timeout          time.Duration
}

func New(ctx context.Context, cfg Config, resolver CredentialResolver, events EventSource) (*Sender, error) {
	if ctx == nil || resolver == nil || strings.TrimSpace(cfg.CredentialRef) == "" || strings.TrimSpace(cfg.Region) == "" || strings.ContainsAny(cfg.Region, " \t\r\n") || cfg.FromAddress == "" || !validConfigurationSet(cfg.ConfigurationSet) {
		return nil, email.ErrSetupRequired
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.RequestTimeout < time.Second || cfg.RequestTimeout > 2*time.Minute {
		return nil, email.ErrSetupRequired
	}
	if _, err := email.NewRenderer(email.RenderConfig{FromAddress: cfg.FromAddress, ApplicationOrigin: cfg.ApplicationOrigin, MaxBodyBytes: email.MaxBodyBytes}); err != nil {
		return nil, email.ErrSetupRequired
	}
	provider, err := resolver.ResolveAWS(ctx, cfg.CredentialRef)
	if err != nil || provider == nil {
		return nil, email.ErrSetupRequired
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region), awsconfig.WithCredentialsProvider(provider), awsconfig.WithRetryMaxAttempts(1))
	if err != nil {
		return nil, email.ErrSetupRequired
	}
	return newSender(awsCfg, cfg, events), nil
}

func newSender(cfg aws.Config, opts Config, events EventSource) *Sender {
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	return &Sender{client: sesv2.NewFromConfig(cfg), events: events, from: opts.FromAddress, configurationSet: opts.ConfigurationSet, timeout: opts.RequestTimeout}
}

func (s *Sender) Send(ctx context.Context, message email.Message) (email.Receipt, error) {
	if s == nil || s.client == nil {
		return receipt.UnknownResult("provider.unavailable"), email.ErrUnavailable
	}
	if ctx == nil || message.JobID() == uuid.Nil || message.Recipient() == "" || message.Sender() != s.from || message.Subject() == "" || message.TextBody() == "" || message.HTMLBody() == "" {
		return receipt.RejectedResult("message.invalid", false, 0), email.ErrInvalidRequest
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	out, err := s.client.SendEmail(requestCtx, &sesv2.SendEmailInput{
		FromEmailAddress:     aws.String(s.from),
		Destination:          &types.Destination{ToAddresses: []string{message.Recipient()}},
		ConfigurationSetName: aws.String(s.configurationSet),
		EmailTags:            []types.MessageTag{{Name: aws.String(JobTagName), Value: aws.String(message.JobID().String())}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: aws.String(message.Subject()), Charset: aws.String("UTF-8")},
			Body:    &types.Body{Text: &types.Content{Data: aws.String(message.TextBody()), Charset: aws.String("UTF-8")}, Html: &types.Content{Data: aws.String(message.HTMLBody()), Charset: aws.String("UTF-8")}},
		}},
	})
	if err != nil {
		return classifySendError(err)
	}
	if out == nil || out.MessageId == nil || !validMessageID(*out.MessageId) {
		return receipt.UnknownResult("provider.receipt_missing"), email.ErrOutcomeUnknown
	}
	return receipt.AcceptedResult(*out.MessageId), nil
}

func classifySendError(err error) (email.Receipt, error) {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		var responseErr *smithyhttp.ResponseError
		if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() >= 400 && responseErr.HTTPStatusCode() < 500 {
			retryable := code == "TooManyRequestsException" || code == "ThrottlingException" || code == "LimitExceededException" || code == "ServiceQuotaExceededException"
			return receipt.RejectedResult("provider.rejected", retryable, ProviderRetryDelay), email.ErrRejected
		}
	}
	return receipt.UnknownResult("provider.outcome_unknown"), email.ErrOutcomeUnknown
}

func validMessageID(id string) bool {
	return id != "" && len(id) <= 256 && strings.IndexFunc(id, unicode.IsControl) < 0
}

func validConfigurationSet(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 64 {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) < 0
}

func (s *Sender) Reconcile(ctx context.Context, jobID uuid.UUID) (email.Receipt, error) {
	if s == nil || s.events == nil {
		return receipt.UnknownResult("provider.event_unavailable"), email.ErrOutcomeUnknown
	}
	if ctx == nil || jobID == uuid.Nil {
		return receipt.UnknownResult("provider.event_unavailable"), email.ErrInvalidRequest
	}
	event, found, err := s.events.LookupVerified(ctx, jobID)
	if err != nil || !found || !event.Verified() || event.JobID() != jobID {
		return receipt.UnknownResult("provider.event_unavailable"), email.ErrOutcomeUnknown
	}
	result := event.Receipt()
	switch result.State() {
	case email.ReceiptAccepted:
		if !validMessageID(result.MessageID()) {
			return receipt.UnknownResult("provider.event_invalid"), email.ErrOutcomeUnknown
		}
		return result, nil
	case email.ReceiptRejected:
		return result, nil
	default:
		return receipt.UnknownResult("provider.event_unresolved"), email.ErrOutcomeUnknown
	}
}
