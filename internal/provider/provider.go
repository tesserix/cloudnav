// Package provider defines the cloud-agnostic interface the TUI renders
// against. A provider wraps a single cloud's CLI and exposes its hierarchy
// (tenants / subscriptions / RGs / resources, or their equivalents) as a tree
// of Node values.
package provider

import (
	"context"
	"time"
)

type Kind string

const (
	KindCloud         Kind = "cloud"
	KindCloudDisabled Kind = "cloud-disabled"
	KindTenant        Kind = "tenant"
	KindSubscription  Kind = "subscription"
	KindResourceGroup Kind = "rg"
	KindResource      Kind = "resource"
	KindProject       Kind = "project"
	KindAccount       Kind = "account"
	KindRegion        Kind = "region"
	// KindFolder is GCP-specific and sits between the cloud root and
	// projects when the user has org-level visibility. Standalone-project
	// users never see this kind — cloudnav falls back to a flat project
	// list.
	KindFolder Kind = "folder"
)

type Node struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Kind     Kind              `json:"kind"`
	Location string            `json:"location,omitempty"`
	State    string            `json:"state,omitempty"`
	Cost     string            `json:"cost,omitempty"`
	Parent   *Node             `json:"-"`
	Meta     map[string]string `json:"meta,omitempty"`
}

// Provider is implemented once per cloud. Implementations live under
// internal/provider/<cloud>/.
type Provider interface {
	Name() string
	LoggedIn(ctx context.Context) error
	Root(ctx context.Context) ([]Node, error)
	Children(ctx context.Context, parent Node) ([]Node, error)
	PortalURL(n Node) string
	Details(ctx context.Context, n Node) ([]byte, error)
}

// Loginer is implemented by providers that know how to invoke their CLI's
// login flow. The TUI uses this to drive `az login` / `gcloud auth login` /
// `aws sso login` from inside the app so first-time users can get credentials
// without leaving the tool. Returning nil argv means the cloud has no login
// handoff and should be instructed manually.
type Loginer interface {
	LoginCommand() (bin string, args []string)
	// InstallHint returns a short human string the UI can show when the CLI
	// itself isn't installed — e.g. "brew install azure-cli".
	InstallHint() string
}

// InstallStep describes one step in installing a cloud CLI. Multi-step
// installs (download + unzip + install) are expressed as a slice.
type InstallStep struct {
	// Description is the short label shown to the user before the step runs.
	Description string
	// Bin + Args form the command to execute.
	Bin  string
	Args []string
	// NeedsSudo hints that the step should be launched with sudo. The runner
	// leaves sudo to the user rather than injecting it silently, so users
	// keep the prompt they expect from their platform.
	NeedsSudo bool
}

// Installer is implemented by providers that can install their own CLI
// non-interactively on the current OS. Returning ok=false means the provider
// doesn't have an automated recipe for this OS and the user should follow the
// InstallHint manually.
type Installer interface {
	InstallPlan(goos string) (steps []InstallStep, ok bool)
}

// Recommendation is a cost / security / reliability / performance tip
// produced by a cloud's advisor-style service — Azure Advisor, Google Cloud
// Recommender, AWS Trusted Advisor etc. Each cloud implements Advisor to
// surface these under the same TUI keybinding.
type Recommendation struct {
	Category     string `json:"category"` // Cost / Security / Reliability / Performance / OperationalExcellence
	Impact       string `json:"impact"`   // High / Medium / Low
	Problem      string `json:"problem"`
	Solution     string `json:"solution"`
	ImpactedName string `json:"impacted,omitempty"`
	ImpactedType string `json:"type,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
	LastUpdated  string `json:"lastUpdated,omitempty"`
}

// Advisor is implemented by providers that can return recommendations for a
// given scope — a subscription, project, or account identifier. The TUI's
// advisor overlay is wired to this.
type Advisor interface {
	Recommendations(ctx context.Context, scopeID string) ([]Recommendation, error)
}

// CostLine is a single row in the billing breakdown overlay — one service,
// region, or project depending on the cloud. Current and LastMonth are in
// the same currency (Currency). The TUI renders delta arrows from these.
type CostLine struct {
	Label     string  `json:"label"`
	Current   float64 `json:"current"`
	LastMonth float64 `json:"lastMonth"`
	Currency  string  `json:"currency"`
	Note      string  `json:"note,omitempty"` // e.g. "no BQ export" or portal link hint
	// Forecast is the projected total spend for the current billing month —
	// current MTD plus the provider's estimate for the remainder of the
	// month. Zero means "no forecast available" (provider doesn't support
	// it, call failed, or first of the month). The TUI renders it as a
	// third value next to last-month / now.
	Forecast float64 `json:"forecast,omitempty"`
	// Budget is the monthly budget ceiling configured on the scope, in the
	// same Currency. Zero means no budget is set. When non-zero the TUI
	// surfaces a 🟢/🟡/🔴 indicator based on Current vs Budget.
	Budget float64 `json:"budget,omitempty"`
}

// Billing is implemented by providers that can produce a flat cost
// breakdown for the current auth scope:
//
//	Azure → subscriptions
//	AWS   → services under the account
//	GCP   → projects under the billing export
//
// Drives the `B` (billing) overlay.
type Billing interface {
	Billing(ctx context.Context) ([]CostLine, error)
}

// BillingScope is an account-wide aggregate that sits above the per-line
// Billing() output. AWS and GCP configure budgets and forecasts at the
// account / billing-account level (not per service or per project), so the
// TUI renders these in the header TOTAL line rather than on each row.
// Azure keeps per-CostLine fields because its budgets are sub-scoped.
type BillingScope struct {
	Forecast float64 `json:"forecast,omitempty"`
	Budget   float64 `json:"budget,omitempty"`
	Currency string  `json:"currency,omitempty"`
	// Note is an optional single-line explanation shown next to the TOTAL
	// when neither forecast nor budget could be resolved (e.g. "no
	// budgets in this account" / "needs Cost Explorer access").
	Note string `json:"note,omitempty"`
}

// BillingSummarer is an optional capability implemented alongside Billing
// to surface account-scope aggregates. The TUI calls BillingSummary()
// after Billing() and uses what it returns to drive the header forecast
// cell and budget indicator.
type BillingSummarer interface {
	BillingSummary(ctx context.Context) (BillingScope, error)
}

// CostHistoryPoint is one day's total cost in a CostSeries.
type CostHistoryPoint struct {
	Date   time.Time `json:"date"`
	Amount float64   `json:"amount"`
}

// CostMonth is the aggregated total for a calendar month across the caller's
// scope. Pre-computed by the provider so the TUI can render MoM deltas
// without re-bucketing.
type CostMonth struct {
	Year  int        `json:"year"`
	Month time.Month `json:"month"`
	Total float64    `json:"total"`
}

// CostSeries is a contiguous daily time-series of cost totals for the
// caller's entire scope (subscriptions / account / billing-account).
// Points are chronologically ordered, oldest first. Gaps should be filled
// with zero so the renderer can draw a continuous line.
type CostSeries struct {
	Label    string             `json:"label"`
	Currency string             `json:"currency"`
	Points   []CostHistoryPoint `json:"points"`
}

// CostHistory is the cloud-agnostic payload powering the `$` cost overlay.
// Series holds the chart points; Months is an independent month-bucketed
// summary the TUI uses for the compare-strip above the chart, regardless
// of how Series itself is bucketed.
type CostHistory struct {
	Scope    string      `json:"scope"`
	Currency string      `json:"currency"`
	Series   CostSeries  `json:"series"`
	Months   []CostMonth `json:"months"`
	// Bucket reflects the granularity of Series.Points. Renderers use
	// this to pick X-axis labels (days of week vs dates vs month names).
	Bucket CostBucket `json:"bucket,omitempty"`
	// Window is the requested window in days; kept around so the UI can
	// label the overlay ("last 7d", "last 12m") without re-computing.
	WindowDays int `json:"windowDays,omitempty"`
	// Note is an optional hint surfaced in the overlay footer when the
	// provider couldn't resolve part of the history (e.g. one sub returned
	// AuthorizationFailed). Renders as muted text.
	Note string `json:"note,omitempty"`
}

// CostBucket controls how CostHistory aggregates its points. Providers
// respect the bucket when they can; they fall back to the closest
// available granularity (e.g. Azure Cost Management has no sub-daily
// bucket, so BucketHour falls back to BucketDay).
type CostBucket string

const (
	// BucketDay is the default — one point per calendar day. Good for
	// windows up to a few months; beyond that the point density overwhelms
	// the chart and BucketWeek or BucketMonth reads better.
	BucketDay CostBucket = "day"
	// BucketWeek buckets points into ISO weeks.
	BucketWeek CostBucket = "week"
	// BucketMonth buckets points into calendar months. Preferred for
	// year-plus windows so a year of data stays within ~12 points.
	BucketMonth CostBucket = "month"
)

// CostHistoryOptions is the request shape for the `$` cost-history
// overlay. Days is the window length in days ending at "now"; Bucket
// decides how the provider aggregates points before handing them back.
// Zero-value means {Days: 90, Bucket: BucketDay} (last 3 months daily).
type CostHistoryOptions struct {
	Days   int
	Bucket CostBucket
}

// CostHistoryer is an optional capability surfacing a cost time-series
// over a configurable window. Drives the `$` overlay and lets the TUI
// offer W / M / 3M / 6M / Y presets that stock-charting tools pioneered.
// Providers that don't implement it leave the overlay disabled; the
// TUI shows a muted hint.
type CostHistoryer interface {
	CostHistory(ctx context.Context, opts CostHistoryOptions) (CostHistory, error)
}

// HealthEvent is a single active incident — service issue, planned
// maintenance, or health advisory — affecting the caller's scope.
// Normalised across clouds so the TUI overlay renders uniformly.
type HealthEvent struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title"`
	Level     string `json:"level"`            // "incident" | "maintenance" | "advisory"
	Status    string `json:"status,omitempty"` // "Active" | "Resolved" etc.
	Service   string `json:"service,omitempty"`
	Region    string `json:"region,omitempty"`
	Scope     string `json:"scope,omitempty"` // subscription / account / project id
	StartTime string `json:"startTime,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// HealthEventer is an optional capability surfacing Service Health /
// incident feeds. Cloud vendors publish these differently — Azure via
// Microsoft.ResourceHealth/events, AWS via AWS Health API, GCP via
// Personalized Service Health — so each provider normalises into
// HealthEvent on behalf of the UI.
type HealthEventer interface {
	HealthEvents(ctx context.Context) ([]HealthEvent, error)
}

// Metric is one named time-series (CPU percent, bytes/s, etc.) attached
// to a single resource. Points are evenly spaced — the provider picks
// the granularity — and aligned to a common window so the TUI's
// sparkline renderer can overlay multiple series without re-binning.
type Metric struct {
	Name   string    `json:"name"`
	Unit   string    `json:"unit"`
	Points []float64 `json:"points"`
}

// Metricser is an optional capability returning short-window time-series
// for a resource. One-shot (not streaming) — the TUI refetches on user
// request rather than maintaining a WebSocket. Clouds implement their
// own semantics: Azure Monitor, CloudWatch, Cloud Monitoring.
type Metricser interface {
	Metrics(ctx context.Context, resource Node) ([]Metric, error)
}

// PIMRole is a single PIM-eligible role assignment. Providers that support
// Privileged Identity Management (or equivalent JIT elevation) implement PIMer.
type PIMRole struct {
	ID               string // eligibility schedule instance id
	RoleName         string
	Scope            string
	ScopeName        string // human-readable scope (e.g. subscription name) when resolvable
	TenantID         string // tenant whose bearer token can activate this role
	PrincipalID      string
	RoleDefinitionID string // /providers/Microsoft.Authorization/roleDefinitions/<guid>
	EligibilityID    string // linkedRoleEligibilityScheduleId
	EndDateTime      string
	Active           bool   // caller currently has an active assignment matching (roleDef, scope)
	ActiveUntil      string // ISO-8601 expiry of the active assignment when Active is true
	MaxDurationHours int    // max activation duration allowed by the PIM policy (0 if unknown)
	// Source narrows which PIM surface this role lives on. Values: "azure"
	// (Azure resource RBAC), "entra" (Microsoft Entra directory roles),
	// "group" (PIM for Groups membership). Default is "azure" for legacy
	// providers that don't set it.
	Source string
	// GroupID is populated when Source == "group" and identifies the PIM-
	// enabled group this eligibility activates membership of.
	GroupID string
}

// PIMer is an optional capability implemented by providers that expose
// just-in-time role elevation. The TUI type-asserts against this to enable
// the p keybinding.
type PIMer interface {
	ListEligibleRoles(ctx context.Context) ([]PIMRole, error)
	ActivateRole(ctx context.Context, role PIMRole, justification string, durationHours int) error
}

// Coster returns formatted month-to-date costs keyed by a child entity name
// (lowercased). Key semantics are provider-specific:
//
//	Azure : parent=subscription, key=resource-group name
//	AWS   : parent=account,      key=service (EC2, S3 ...)
//	GCP   : parent=cloud,        key=project id (aggregate across billing accts)
type Coster interface {
	Costs(ctx context.Context, parent Node) (map[string]string, error)
}

// VM is a provider-agnostic view of a compute instance.
type VM struct {
	ID       string
	Name     string
	State    string
	Type     string
	Location string
	Meta     map[string]string
}

// VMOps is the capability implemented by providers that can read or control
// VMs / EC2 / Compute Engine instances.
type VMOps interface {
	ListVMs(ctx context.Context, scope Node) ([]VM, error)
	ShowVM(ctx context.Context, id, scope string) ([]byte, error)
	StartVM(ctx context.Context, id, scope string) error
	StopVM(ctx context.Context, id, scope string) error
}
