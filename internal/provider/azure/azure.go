// Package azure implements provider.Provider by wrapping the `az` CLI.
package azure

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type Azure struct {
	az *cli.Runner
}

func New() *Azure {
	return &Azure{az: cli.New("az")}
}

func (a *Azure) Name() string { return "azure" }

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
	var subs []subJSON
	if err := json.Unmarshal(out, &subs); err != nil {
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
	var rgs []rgJSON
	if err := json.Unmarshal(out, &rgs); err != nil {
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
	var items []resJSON
	if err := json.Unmarshal(out, &items); err != nil {
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
