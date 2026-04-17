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
