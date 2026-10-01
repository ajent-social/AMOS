// Package catalog holds reviewed, server-side flat-price catalog versions and
// evaluates paid-feature access from fresh, scoped subscription projections.
// The initial supported currency set is USD with two minor-unit digits; other
// currencies remain rejected until their scale and provider handling are reviewed.
package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ajent-social/amos/billing/provider"
	billingstore "github.com/ajent-social/amos/billing/store"
	"github.com/google/uuid"
)

var (
	ErrInvalidCatalog = errors.New("invalid billing catalog")
	ErrDuplicateKey   = errors.New("duplicate catalog key")
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

// supportedCurrencyScales contains only currency scales qualified for this
// catalog implementation. Extending currency support requires adding reviewed
// provider/account and rounding tests; valid-looking ISO codes alone are not support.
var supportedCurrencyScales = map[string]int{"USD": 2}

type Model string

const ModelFlat Model = "flat"

type Interval string

const (
	IntervalMonth Interval = "month"
	IntervalYear  Interval = "year"
)

type Outcome string

const (
	OutcomeAllowed     Outcome = "allowed"
	OutcomeDenied      Outcome = "denied"
	OutcomeUnavailable Outcome = "unavailable"
)

type Reason string

const (
	ReasonEntitled          Reason = "entitled"
	ReasonEssentialPath     Reason = "essential_path"
	ReasonFeatureNotInPlan  Reason = "feature_not_in_plan"
	ReasonPermissionDenied  Reason = "tenant_permission_denied"
	ReasonScopeMismatch     Reason = "scope_mismatch"
	ReasonNoProjection      Reason = "projection_missing"
	ReasonStaleProjection   Reason = "projection_stale"
	ReasonUnknownPrice      Reason = "price_unrecognized"
	ReasonUnconfirmed       Reason = "subscription_unconfirmed"
	ReasonOutsidePeriod     Reason = "outside_paid_period"
	ReasonDelinquent        Reason = "delinquent"
	ReasonCanceled          Reason = "subscription_canceled"
	ReasonPriceNotEffective Reason = "price_not_effective"
)

type Scope struct {
	InstallationID    string
	ApplicationID     string
	EnvironmentID     string
	WorkspaceID       string
	Provider          string
	ProviderAccountID string
	AccountMode       provider.AccountMode
}

func (s Scope) validate() error {
	b := provider.Binding{InstallationID: s.InstallationID, EnvironmentID: s.EnvironmentID,
		WorkspaceID: s.WorkspaceID, Provider: s.Provider, AccountID: s.ProviderAccountID, AccountMode: s.AccountMode}
	if b.Validate() != nil || !validUUIDv7(s.ApplicationID) {
		return ErrInvalidCatalog
	}
	return nil
}

// catalogScope omits workspace because plans are shared; workspace binding is
// checked independently against each verified subscription projection.
func (s Scope) catalogScope() Scope {
	s.WorkspaceID = ""
	return s
}
func (s Scope) validateCatalogScope() error {
	if !validUUIDv7(s.InstallationID) || !validUUIDv7(s.ApplicationID) || !validUUIDv7(s.EnvironmentID) ||
		!validIdentifier(s.Provider) || !validIdentifier(s.ProviderAccountID) ||
		(s.AccountMode != provider.AccountTest && s.AccountMode != provider.AccountLive) {
		return ErrInvalidCatalog
	}
	return nil
}

type Price struct {
	Key              string // selection key accepted by trusted server code; never an amount from a request
	ProviderPriceKey string // configured provider price identifier stored in the projection
	AmountMinor      int64
	Interval         Interval
}

type Plan struct {
	Key           string
	Revision      string
	ProductFamily string
	Model         Model
	Scope         Scope
	Currency      string
	MinorUnit     int
	EffectiveAt   time.Time
	WithdrawnAt   time.Time // stops new selections; retained versions remain evaluable
	Features      []string
	Prices        []Price
}

type Config struct {
	Revision  string
	Freshness time.Duration
	Plans     []Plan
}

type Catalog struct {
	revision       string
	scope          Scope
	freshness      time.Duration
	plans          map[string]Plan
	selections     map[string]Price
	providerPrices map[string]planPrice
}

type planPrice struct {
	plan  Plan
	price Price
}

// New validates and copies reviewed configuration. Returned maps/slices are
// private so callers cannot mutate a live catalog after construction.
func New(cfg Config) (*Catalog, error) {
	if !validIdentifier(cfg.Revision) || len(cfg.Plans) == 0 {
		return nil, ErrInvalidCatalog
	}
	if cfg.Freshness == 0 {
		cfg.Freshness = 5 * time.Minute
	}
	if cfg.Freshness < time.Second || cfg.Freshness > 5*time.Minute {
		return nil, ErrInvalidCatalog
	}
	c := &Catalog{revision: cfg.Revision, freshness: cfg.Freshness, plans: map[string]Plan{}, selections: map[string]Price{}, providerPrices: map[string]planPrice{}}
	for _, raw := range cfg.Plans {
		p := clonePlan(raw)
		if err := validatePlan(p); err != nil {
			return nil, fmt.Errorf("%w: plan %q", err, p.Key)
		}
		if len(c.plans) == 0 {
			c.scope = p.Scope.catalogScope()
		} else if p.Scope.catalogScope() != c.scope {
			return nil, ErrInvalidCatalog
		}
		if _, ok := c.plans[p.Key]; ok {
			return nil, fmt.Errorf("%w: plan %s", ErrDuplicateKey, p.Key)
		}
		c.plans[p.Key] = p
		for _, price := range p.Prices {
			if _, ok := c.selections[price.Key]; ok {
				return nil, fmt.Errorf("%w: selection %s", ErrDuplicateKey, price.Key)
			}
			if _, ok := c.providerPrices[price.ProviderPriceKey]; ok {
				return nil, fmt.Errorf("%w: provider price", ErrDuplicateKey)
			}
			c.selections[price.Key] = price
			c.providerPrices[price.ProviderPriceKey] = planPrice{plan: p, price: price}
		}
	}
	return c, nil
}

func (c *Catalog) Revision() string {
	if c == nil {
		return ""
	}
	return c.revision
}

// ResolveSelection maps a trusted server-side selection to its immutable
// amount/currency/provider price tuple. Withdrawn prices cannot start checkout.
func (c *Catalog) ResolveSelection(key string, at time.Time) (Plan, Price, error) {
	if c == nil || at.IsZero() {
		return Plan{}, Price{}, ErrInvalidCatalog
	}
	p, ok := c.findSelection(key)
	if !ok {
		return Plan{}, Price{}, ErrInvalidCatalog
	}
	plan := c.plansForSelection(key)
	if !plan.WithdrawnAt.IsZero() && !at.Before(plan.WithdrawnAt) || at.Before(plan.EffectiveAt) {
		return Plan{}, Price{}, ErrInvalidCatalog
	}
	return clonePlan(plan), p, nil
}

func (c *Catalog) findSelection(key string) (Price, bool) { p, ok := c.selections[key]; return p, ok }
func (c *Catalog) plansForSelection(key string) Plan {
	price := c.selections[key]
	for _, p := range c.plans {
		for _, v := range p.Prices {
			if v.Key == price.Key {
				return p
			}
		}
	}
	return Plan{}
}

type EssentialPath string

const (
	PathAccount            EssentialPath = "account"
	PathRecovery           EssentialPath = "recovery"
	PathBillingRestoration EssentialPath = "billing_restoration"
)

type Permission string

const (
	PermissionGranted Permission = "granted"
	PermissionDenied  Permission = "denied"
)

type Decision struct {
	Outcome            Outcome
	Reason             Reason
	Feature            string
	PlanKey            string
	PlanRevision       string
	ProductFamily      string
	Model              Model
	Currency           string
	MinorUnit          int
	AmountMinor        int64
	Interval           Interval
	CatalogRevision    string
	ObservedAt         time.Time
	ValidUntil         time.Time
	PermissionRequired bool
}

// AuthorizesBusinessAction combines the entitlement result with the separate
// tenant policy result. It never treats an entitlement as tenant permission.
func (d Decision) AuthorizeBusinessAction(permission Permission) Decision {
	if d.Outcome != OutcomeAllowed || !d.PermissionRequired {
		return d
	}
	if permission != PermissionGranted {
		d.Outcome = OutcomeDenied
		d.Reason = ReasonPermissionDenied
	}
	return d
}

// Evaluate computes a paid-feature decision. Scope and Now come from trusted
// application context; they are not accepted as client billing claims.
func (c *Catalog) Evaluate(scope Scope, projection *billingstore.SubscriptionProjection, feature string, now time.Time) Decision {
	base := Decision{Outcome: OutcomeUnavailable, Reason: ReasonNoProjection, Feature: feature, PermissionRequired: true}
	if c == nil {
		return base
	}
	base.CatalogRevision = c.revision
	if scope.validate() != nil || scope.catalogScope() != c.scope {
		base.Reason = ReasonScopeMismatch
		return base
	}
	if !validIdentifier(feature) || now.IsZero() {
		base.Reason = ReasonScopeMismatch
		return base
	}
	if projection == nil {
		return base
	}
	if !sameScope(scope, projection.Binding) {
		base.Reason = ReasonScopeMismatch
		return base
	}
	if projection.ObservedAt.IsZero() || projection.ObservedAt.After(now) {
		base.Reason = ReasonUnconfirmed
		return base
	}
	validUntil := projection.ObservedAt.Add(c.freshness)
	if !projection.PeriodEnd.IsZero() && projection.PeriodEnd.Before(validUntil) {
		validUntil = projection.PeriodEnd
	}
	base.ObservedAt = projection.ObservedAt
	base.ValidUntil = validUntil
	entry, ok := c.providerPrices[projection.PriceKey]
	if !ok || entry.plan.Scope != scope.catalogScope() {
		base.Reason = ReasonUnknownPrice
		return base
	}
	base.PlanKey = entry.plan.Key
	base.PlanRevision = entry.plan.Revision
	base.ProductFamily = entry.plan.ProductFamily
	base.Model = entry.plan.Model
	base.Currency = entry.plan.Currency
	base.MinorUnit = entry.plan.MinorUnit
	base.AmountMinor = entry.price.AmountMinor
	base.Interval = entry.price.Interval
	if now.Before(entry.plan.EffectiveAt) {
		base.Reason = ReasonPriceNotEffective
		return base
	}
	if !contains(entry.plan.Features, feature) {
		base.Outcome = OutcomeDenied
		base.Reason = ReasonFeatureNotInPlan
		return base
	}
	if projection.PeriodStart.IsZero() || projection.PeriodEnd.IsZero() || now.Before(projection.PeriodStart) || !now.Before(projection.PeriodEnd) {
		base.Outcome = OutcomeDenied
		base.Reason = ReasonOutsidePeriod
		return base
	}
	switch projection.State {
	case "confirmed":
	case "canceled":
		base.Outcome = OutcomeDenied
		base.Reason = ReasonCanceled
		return base
	default:
		if projection.State == "past_due" {
			base.Outcome = OutcomeDenied
			base.Reason = ReasonDelinquent
			return base
		}
		if projection.State == "failed" {
			base.Outcome = OutcomeDenied
			base.Reason = ReasonUnconfirmed
			return base
		}
		base.Reason = ReasonUnconfirmed
		return base
	}
	if !now.Before(validUntil) {
		base.Reason = ReasonStaleProjection
		return base
	}
	base.Outcome = OutcomeAllowed
	base.Reason = ReasonEntitled
	return base
}

// EvaluateEssentialPath leaves account, recovery, and billing restoration
// usable during paid-feature denial. Normal authentication and tenant policy
// still apply outside this package.
func EvaluateEssentialPath(path EssentialPath) Decision {
	switch path {
	case PathAccount, PathRecovery, PathBillingRestoration:
		return Decision{Outcome: OutcomeAllowed, Reason: ReasonEssentialPath}
	default:
		return Decision{Outcome: OutcomeDenied, Reason: ReasonFeatureNotInPlan}
	}
}

func sameScope(s Scope, b provider.Binding) bool {
	return s.InstallationID == b.InstallationID && s.EnvironmentID == b.EnvironmentID && s.WorkspaceID == b.WorkspaceID && s.Provider == b.Provider && s.ProviderAccountID == b.AccountID && s.AccountMode == b.AccountMode
}
func validUUIDv7(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u.Version() == 7 && u.Variant() == uuid.RFC4122 && u.String() == s
}
func validatePlan(p Plan) error {
	minorUnit, currencySupported := supportedCurrencyScales[p.Currency]
	if !currencySupported || minorUnit != p.MinorUnit {
		return ErrInvalidCatalog
	}
	if !validIdentifier(p.Key) || !validIdentifier(p.Revision) || !validIdentifier(p.ProductFamily) ||
		p.Model != ModelFlat || p.Scope.validateCatalogScope() != nil || p.EffectiveAt.IsZero() ||
		(!p.WithdrawnAt.IsZero() && !p.WithdrawnAt.After(p.EffectiveAt)) || len(p.Features) == 0 || len(p.Prices) == 0 {
		return ErrInvalidCatalog
	}
	seen := map[string]bool{}
	for _, f := range p.Features {
		if !validIdentifier(f) || seen[f] {
			return ErrInvalidCatalog
		}
		seen[f] = true
	}
	intervals := map[Interval]bool{}
	for _, price := range p.Prices {
		if !validIdentifier(price.Key) || !validIdentifier(price.ProviderPriceKey) || price.AmountMinor <= 0 || (price.Interval != IntervalMonth && price.Interval != IntervalYear) || intervals[price.Interval] {
			return ErrInvalidCatalog
		}
		intervals[price.Interval] = true
	}
	if !intervals[IntervalMonth] || !intervals[IntervalYear] {
		return ErrInvalidCatalog
	}
	return nil
}
func clonePlan(p Plan) Plan {
	p.Features = append([]string(nil), p.Features...)
	p.Prices = append([]Price(nil), p.Prices...)
	return p
}
func validIdentifier(s string) bool {
	return identifierPattern.MatchString(strings.TrimSpace(s)) && s == strings.TrimSpace(s)
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
