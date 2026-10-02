package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ajent-social/amos/billing/catalog"
	"github.com/ajent-social/amos/billing/reconcile"
	"github.com/ajent-social/amos/billing/stripecheckout"
	reference "github.com/ajent-social/amos/examples/reference/migrations"
	"github.com/ajent-social/amos/identity/federation"
	"github.com/ajent-social/amos/identity/mfa"
	"github.com/ajent-social/amos/jobs"
	"github.com/ajent-social/amos/jobs/sqlstore"
	"github.com/ajent-social/amos/migrations"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

// Configuration is owner-authored. Credentials are read separately and never
// serialized or included in diagnostics. Billing is an explicit opt-in service.
type billingServiceConfig struct {
	Catalog     catalog.Config `json:"catalog"`
	ReturnHosts []string       `json:"return_hosts"`
}

func runBilling(args []string) int {
	flags := flag.NewFlagSet("billing-reconcile", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	path := flags.String("config", "", "owner-authored billing service JSON")
	once := flags.Bool("once", false, "run one bounded scheduling pass")
	migrate := flags.Bool("migrate-only", false, "append billing migrations and exit")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *path == "" || (*once && *migrate) {
		return 2
	}
	cfg, err := readBillingConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "amos billing-reconcile: invalid owner configuration")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := serveBilling(ctx, cfg, *once, *migrate); err != nil {
		fmt.Fprintln(os.Stderr, "amos billing-reconcile: configuration, dependency or scheduling unavailable")
		return 1
	}
	return 0
}

func readBillingConfig(path string) (billingServiceConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return billingServiceConfig{}, err
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || uniqueBillingJSON(json.NewDecoder(bytes.NewReader(body)), 0) != nil {
		return billingServiceConfig{}, errors.New("invalid configuration")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var cfg billingServiceConfig
	if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF {
		return billingServiceConfig{}, errors.New("invalid configuration")
	}
	if _, err := catalog.New(cfg.Catalog); err != nil || len(cfg.ReturnHosts) == 0 || cfg.Catalog.Plans[0].Scope.Provider != "stripe" {
		return billingServiceConfig{}, errors.New("invalid configuration")
	}
	return cfg, nil
}

func serveBilling(ctx context.Context, cfg billingServiceConfig, once, migrateOnly bool) error {
	databaseVariable := "AMOS_BILLING_DATABASE_URL"
	if migrateOnly {
		databaseVariable = "AMOS_MIGRATION_DATABASE_URL"
	}
	db, err := storage.Open(ctx, os.Getenv(databaseVariable))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if migrateOnly {
		registry, err := billingMigrationRegistry()
		if err != nil {
			return err
		}
		return storage.Migrate(ctx, db, registry)
	}
	terms, err := catalog.New(cfg.Catalog)
	if err != nil {
		return err
	}
	scope := cfg.Catalog.Plans[0].Scope
	adapter, err := stripecheckout.New(stripecheckout.Config{APIKey: os.Getenv("AMOS_STRIPE_API_KEY"), ProviderAccountID: scope.ProviderAccountID, AccountMode: scope.AccountMode, Catalog: terms, ReturnHosts: cfg.ReturnHosts, Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	keys := make(map[string]struct{})
	for _, plan := range cfg.Catalog.Plans {
		for _, price := range plan.Prices {
			keys[price.ProviderPriceKey] = struct{}{}
		}
	}
	endpoint := reconcile.ScanScope{InstallationID: uuid.MustParse(scope.InstallationID), ApplicationID: uuid.MustParse(scope.ApplicationID), EnvironmentID: uuid.MustParse(scope.EnvironmentID), Provider: scope.Provider, ProviderAccountID: scope.ProviderAccountID, AccountMode: scope.AccountMode}
	worker, err := reconcile.New(db, adapter, reconcile.Config{Scopes: []reconcile.ScanScope{endpoint}, Lease: time.Minute, RequestTimeout: 10 * time.Second, Freshness: 5 * time.Minute, RefreshAfter: time.Minute, RetryBase: time.Second, RetryMax: time.Minute, MaxAttempts: 5, MinRequestInterval: time.Second, PriceKeys: keys})
	if err != nil {
		return err
	}
	consumer, err := reconcile.NewWebhookJobConsumer(reconcile.WebhookJobConfig{Database: db, Endpoints: []reconcile.ScanScope{endpoint}})
	if err != nil {
		return err
	}
	pool, err := sql.Open("pgx", os.Getenv(databaseVariable))
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	pool.SetMaxOpenConns(4)
	store, err := sqlstore.New(pool, sqlstore.Config{ClaimKinds: []string{"billing.webhook.reconcile"}, ClaimScopes: []sqlstore.ClaimScope{{InstallationID: endpoint.InstallationID, ApplicationID: endpoint.ApplicationID, EnvironmentID: endpoint.EnvironmentID, Provider: endpoint.Provider, ProviderAccountID: endpoint.ProviderAccountID, AccountMode: string(endpoint.AccountMode)}}, MaxPayloadBytes: 2048, MaxAttempts: 5, MaxReconciliationAttempts: 5, MaxLease: time.Minute, MaxRetryDelay: time.Hour})
	if err != nil {
		return err
	}
	owner, err := uuid.NewV7()
	if err != nil {
		return err
	}
	scheduler, err := reconcile.NewScheduler(reconcile.SchedulerConfig{Database: db, Reconciler: worker, WebhookWorker: jobs.Worker{Repository: store, Consumer: consumer, Owner: "billing-" + owner.String(), Lease: time.Minute}, Endpoints: []reconcile.ScanScope{endpoint}, PollInterval: time.Second, ScanInterval: time.Minute, ScanPageSize: 100, MaxWebhookJobsPerPass: 10, MaxReconciliationsPerPass: 10, MaxScanPagesPerPass: 1, OnError: func(error) { fmt.Fprintln(os.Stderr, "amos billing-reconcile: bounded pass failed; retrying") }})
	if err != nil {
		return err
	}
	if once {
		_, err = scheduler.RunOne(ctx)
		return err
	}
	err = scheduler.Run(ctx)
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func billingMigrationRegistry() (migrations.Registry, error) {
	ingress, err := migrations.BillingWebhookIngress(9)
	if err != nil {
		return migrations.Registry{}, err
	}
	binding, err := migrations.RuntimeBinding(10)
	if err != nil {
		return migrations.Registry{}, err
	}
	magic, err := migrations.MagicBrowserBinding(11)
	if err != nil {
		return migrations.Registry{}, err
	}
	assurance, err := migrations.SessionAssurance(12)
	if err != nil {
		return migrations.Registry{}, err
	}
	factors, err := mfa.Fragment(13)
	if err != nil {
		return migrations.Registry{}, err
	}
	limits, err := migrations.MFAProtection(14)
	if err != nil {
		return migrations.Registry{}, err
	}
	reconciliation, err := reconcile.Schema(15)
	if err != nil {
		return migrations.Registry{}, err
	}
	federationFlow, err := federation.Fragment(16)
	if err != nil {
		return migrations.Registry{}, err
	}
	return migrations.Core(reference.Fragment(), ingress, binding, magic, assurance, factors, limits, reconciliation, federationFlow)
}

// Reject ambiguous duplicate keys and excessive nesting before typed decoding.
func uniqueBillingJSON(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("configuration nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[strings.ToLower(name)] {
				return errors.New("duplicate configuration key")
			}
			seen[strings.ToLower(name)] = true
			if err := uniqueBillingJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueBillingJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid configuration delimiter")
	}
	_, err = decoder.Token()
	return err
}
