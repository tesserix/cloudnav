package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armlocks"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// listLocksSDK lists every lock in a subscription and buckets them by
// RG name (lowercased), mirroring ResourceGroupLocks' contract.
func (a *Azure) listLocksSDK(ctx context.Context, subID string) (map[string][]Lock, error) {
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return nil, err
	}
	client, err := armlocks.NewManagementLocksClient(subID, cred, nil)
	if err != nil {
		return nil, err
	}
	pager := client.NewListAtSubscriptionLevelPager(nil)
	out := map[string][]Lock{}
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list locks: %w", err)
		}
		for _, l := range page.Value {
			if l == nil || l.ID == nil || l.Name == nil {
				continue
			}
			rg := rgFromScope(*l.ID)
			if rg == "" {
				continue
			}
			level := ""
			if l.Properties != nil && l.Properties.Level != nil {
				level = string(*l.Properties.Level)
			}
			key := strings.ToLower(rg)
			out[key] = append(out[key], Lock{
				Name:  *l.Name,
				Level: level,
				Scope: *l.ID,
				RG:    rg,
			})
		}
	}
	return out, nil
}

// createRGLockSDK puts a management lock on a resource group.
func (a *Azure) createRGLockSDK(ctx context.Context, subID, rg, name, level string) error {
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return err
	}
	client, err := armlocks.NewManagementLocksClient(subID, cred, nil)
	if err != nil {
		return err
	}
	_, err = client.CreateOrUpdateAtResourceGroupLevel(ctx, rg, name, armlocks.ManagementLockObject{
		Properties: &armlocks.ManagementLockProperties{
			Level: to.Ptr(armlocks.LockLevel(level)),
		},
	}, nil)
	return err
}

// deleteRGLockSDK removes a lock by name from a resource group.
func (a *Azure) deleteRGLockSDK(ctx context.Context, subID, rg, lockName string) error {
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return err
	}
	client, err := armlocks.NewManagementLocksClient(subID, cred, nil)
	if err != nil {
		return err
	}
	_, err = client.DeleteAtResourceGroupLevel(ctx, rg, lockName, nil)
	return err
}

// deleteResourceGroupSDK issues an async BeginDelete. We don't block
// on the LRO — the CLI path used --no-wait and we keep the same UX.
func (a *Azure) deleteResourceGroupSDK(ctx context.Context, subID, rg string) error {
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return err
	}
	client, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return err
	}
	_, err = client.BeginDelete(ctx, rg, nil)
	return err
}
