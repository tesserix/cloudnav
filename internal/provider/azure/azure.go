// Package azure implements provider.Provider by wrapping the `az` CLI.
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type Azure struct {
	az *cli.Runner

	mu      sync.RWMutex
	tenants map[string]string // tenantId → displayName
	subs    map[string]string // subscriptionId → name
}

func New() *Azure {
	r := cli.New("az")
	r.Timeout = 2 * time.Minute
	return &Azure{az: r}
}

func (a *Azure) Name() string { return "azure" }

// tenantName returns the cached display name for a tenant or "" if unknown.
func (a *Azure) tenantName(id string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tenants[id]
}

// subName returns the cached name for a subscription or "" if unknown.
func (a *Azure) subName(id string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.subs[id]
}

func (a *Azure) putSubs(m map[string]string) {
	a.mu.Lock()
	a.subs = m
	a.mu.Unlock()
}

func (a *Azure) putTenants(m map[string]string) {
	a.mu.Lock()
	a.tenants = m
	a.mu.Unlock()
}

// fetchTenants is best-effort — failure is non-fatal, we just fall back to
// showing the tenantId when rendering.
func (a *Azure) fetchTenants(ctx context.Context) {
	out, err := a.az.Run(ctx, "rest", "--method", "GET",
		"--url", "https://management.azure.com/tenants?api-version=2022-09-01")
	if err != nil {
		return
	}
	var env struct {
		Value []struct {
			TenantID    string `json:"tenantId"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return
	}
	m := make(map[string]string, len(env.Value))
	for _, t := range env.Value {
		m[t.TenantID] = t.DisplayName
	}
	a.putTenants(m)
}

func (a *Azure) LoggedIn(ctx context.Context) error {
	_, err := a.az.Run(ctx, "account", "show", "-o", "json")
	return err
}

type subJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	TenantID string `json:"tenantId"`
	User     struct {
		Name string `json:"name"`
	} `json:"user"`
}

func (a *Azure) Root(ctx context.Context) ([]provider.Node, error) {
	out, err := a.az.Run(ctx, "account", "list", "-o", "json")
	if err != nil {
		return nil, err
	}
	a.fetchTenants(ctx)
	nodes, err := parseSubs(out)
	if err != nil {
		return nil, err
	}
	subCache := make(map[string]string, len(nodes))
	for i := range nodes {
		subCache[nodes[i].ID] = nodes[i].Name
		if name := a.tenantName(nodes[i].Meta["tenantId"]); name != "" {
			nodes[i].Meta["tenantName"] = name
		}
	}
	a.putSubs(subCache)
	return nodes, nil
}

func parseSubs(data []byte) ([]provider.Node, error) {
	var subs []subJSON
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(subs))
	for _, s := range subs {
		nodes = append(nodes, provider.Node{
			ID:    s.ID,
			Name:  s.Name,
			Kind:  provider.KindSubscription,
			State: s.State,
			Meta: map[string]string{
				"tenantId": s.TenantID,
				"user":     s.User.Name,
			},
		})
	}
	return nodes, nil
}

// subIDFromScope extracts the subscription UUID from an Azure resource scope
// like "/subscriptions/<uuid>/resourceGroups/...".
func subIDFromScope(scope string) string {
	const prefix = "/subscriptions/"
	rest := strings.TrimPrefix(scope, prefix)
	if rest == scope {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (a *Azure) Children(ctx context.Context, parent provider.Node) ([]provider.Node, error) {
	switch parent.Kind {
	case provider.KindSubscription:
		return a.resourceGroups(ctx, parent)
	case provider.KindResourceGroup:
		return a.resources(ctx, parent)
	default:
		return nil, fmt.Errorf("azure: no children for kind %q", parent.Kind)
	}
}

type rgJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
	} `json:"properties"`
}

func (a *Azure) resourceGroups(ctx context.Context, sub provider.Node) ([]provider.Node, error) {
	out, err := a.az.Run(ctx, "group", "list", "--subscription", sub.ID, "-o", "json")
	if err != nil {
		return nil, err
	}
	return parseRGs(out, sub)
}

func parseRGs(data []byte, sub provider.Node) ([]provider.Node, error) {
	var rgs []rgJSON
	if err := json.Unmarshal(data, &rgs); err != nil {
		return nil, fmt.Errorf("parse az group list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(rgs))
	parent := sub
	for _, r := range rgs {
		nodes = append(nodes, provider.Node{
			ID:       r.ID,
			Name:     r.Name,
			Kind:     provider.KindResourceGroup,
			Location: r.Location,
			State:    r.Properties.ProvisioningState,
			Parent:   &parent,
			Meta: map[string]string{
				"tenantId":       sub.Meta["tenantId"],
				"subscriptionId": sub.ID,
			},
		})
	}
	return nodes, nil
}

type resJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Type     string `json:"type"`
}

func (a *Azure) resources(ctx context.Context, rg provider.Node) ([]provider.Node, error) {
	subID := rg.Meta["subscriptionId"]
	if subID == "" && rg.Parent != nil {
		subID = rg.Parent.ID
	}
	if subID == "" {
		return nil, fmt.Errorf("azure: resource group %q has no subscription context", rg.Name)
	}
	out, err := a.az.Run(ctx,
		"resource", "list",
		"--resource-group", rg.Name,
		"--subscription", subID,
		"-o", "json",
	)
	if err != nil {
		return nil, err
	}
	return parseResources(out, rg, subID)
}

func parseResources(data []byte, rg provider.Node, subID string) ([]provider.Node, error) {
	var items []resJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse az resource list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(items))
	parent := rg
	for _, r := range items {
		nodes = append(nodes, provider.Node{
			ID:       r.ID,
			Name:     r.Name,
			Kind:     provider.KindResource,
			Location: r.Location,
			State:    shortType(r.Type),
			Parent:   &parent,
			Meta: map[string]string{
				"type":           r.Type,
				"tenantId":       rg.Meta["tenantId"],
				"subscriptionId": subID,
			},
		})
	}
	return nodes, nil
}

func (a *Azure) PortalURL(n provider.Node) string {
	base := "https://portal.azure.com"
	if t := n.Meta["tenantId"]; t != "" {
		base += "/#@" + t
	} else {
		base += "/#"
	}
	switch n.Kind {
	case provider.KindSubscription, provider.KindResourceGroup, provider.KindResource:
		return base + "/resource" + n.ID
	default:
		return base
	}
}

func (a *Azure) Details(ctx context.Context, n provider.Node) ([]byte, error) {
	switch n.Kind {
	case provider.KindResource, provider.KindResourceGroup:
		return a.az.Run(ctx, "resource", "show", "--ids", n.ID, "-o", "json")
	case provider.KindSubscription:
		return a.az.Run(ctx, "account", "show", "--subscription", n.ID, "-o", "json")
	default:
		return nil, fmt.Errorf("azure: no detail view for kind %q", n.Kind)
	}
}

// shortType trims "Microsoft.Compute/virtualMachines" to "virtualMachines".
func shortType(t string) string {
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == '/' {
			return t[i+1:]
		}
	}
	return t
}
