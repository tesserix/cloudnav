package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/tesserix/cloudnav/internal/provider"
)

// subCredential returns a TokenCredential scoped to the subscription's
// home tenant. Used by the per-sub armresources clients so cross-tenant
// subscriptions auth with the right directory.
func (a *Azure) subCredential(ctx context.Context, subID string) (azcore.TokenCredential, error) {
	tid := a.tenantForSub(ctx, subID)
	if tid == "" {
		return nil, fmt.Errorf("azure: could not resolve tenant for subscription %s", subID)
	}
	return tenantCLICred(tid)
}

// listResourceGroupsSDK fetches resource groups for a subscription via
// armresources. Replaces `az group list --subscription <id>`.
func (a *Azure) listResourceGroupsSDK(ctx context.Context, sub provider.Node) ([]provider.Node, error) {
	cred, err := a.subCredential(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	client, err := armresources.NewResourceGroupsClient(sub.ID, cred, nil)
	if err != nil {
		return nil, err
	}
	pager := client.NewListPager(nil)
	parent := sub
	var nodes []provider.Node
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list resource groups: %w", err)
		}
		for _, g := range page.Value {
			if g == nil || g.Name == nil {
				continue
			}
			state := ""
			if g.Properties != nil && g.Properties.ProvisioningState != nil {
				state = *g.Properties.ProvisioningState
			}
			location := ""
			if g.Location != nil {
				location = *g.Location
			}
			meta := map[string]string{
				"tenantId":       sub.Meta["tenantId"],
				"subscriptionId": sub.ID,
			}
			if tags := tagsFromPointers(g.Tags); tags != "" {
				meta["tags"] = tags
			}
			id := ""
			if g.ID != nil {
				id = *g.ID
			}
			nodes = append(nodes, provider.Node{
				ID:       id,
				Name:     *g.Name,
				Kind:     provider.KindResourceGroup,
				Location: location,
				State:    state,
				Parent:   &parent,
				Meta:     meta,
			})
		}
	}
	return nodes, nil
}

// listResourcesInRGSDK fetches a single RG's resources. Replaces
// `az resource list --resource-group <rg> --subscription <sub>`.
// The $expand=createdTime,changedTime hint matches what the CLI path
// passes, giving us per-resource audit timestamps.
func (a *Azure) listResourcesInRGSDK(ctx context.Context, sub provider.Node, rg provider.Node) ([]provider.Node, error) {
	cred, err := a.subCredential(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	client, err := armresources.NewClient(sub.ID, cred, nil)
	if err != nil {
		return nil, err
	}
	pager := client.NewListByResourceGroupPager(rg.Name, &armresources.ClientListByResourceGroupOptions{
		Expand: to.Ptr("createdTime,changedTime"),
	})
	parent := rg
	var nodes []provider.Node
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list resources in %s: %w", rg.Name, err)
		}
		for _, r := range page.Value {
			if r == nil || r.ID == nil {
				continue
			}
			name := ""
			if r.Name != nil {
				name = *r.Name
			}
			typ := ""
			if r.Type != nil {
				typ = *r.Type
			}
			location := ""
			if r.Location != nil {
				location = *r.Location
			}
			meta := map[string]string{
				"type":           typ,
				"tenantId":       rg.Meta["tenantId"],
				"subscriptionId": sub.ID,
			}
			if r.CreatedTime != nil {
				meta["createdTime"] = r.CreatedTime.Format("2006-01-02T15:04:05Z")
			}
			if r.ChangedTime != nil {
				meta["changedTime"] = r.ChangedTime.Format("2006-01-02T15:04:05Z")
			}
			if tags := tagsFromPointers(r.Tags); tags != "" {
				meta["tags"] = tags
			}
			nodes = append(nodes, provider.Node{
				ID:       *r.ID,
				Name:     name,
				Kind:     provider.KindResource,
				Location: location,
				State:    shortType(typ),
				Parent:   &parent,
				Meta:     meta,
			})
		}
	}
	return nodes, nil
}

// tagsFromPointers renders a map[string]*string (Azure SDK tag shape)
// to the deterministic "k1=v1,k2=v2" string the rest of the TUI uses.
func tagsFromPointers(tags map[string]*string) string {
	if len(tags) == 0 {
		return ""
	}
	flat := make(map[string]string, len(tags))
	for k, v := range tags {
		if v != nil {
			flat[k] = *v
		}
	}
	return formatTags(flat)
}
