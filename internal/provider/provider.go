// Package provider defines the cloud-agnostic interface the TUI renders
// against. A provider wraps a single cloud's CLI and exposes its hierarchy
// (tenants / subscriptions / RGs / resources, or their equivalents) as a tree
// of Node values.
package provider

import "context"

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
