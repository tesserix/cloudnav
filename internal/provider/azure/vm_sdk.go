package azure

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"

	"github.com/tesserix/cloudnav/internal/provider"
)

// listVMsSDK lists VMs at a subscription or resource-group scope using
// armcompute. Returns ok=false when the credential can't resolve so the
// caller falls back to 'az vm list'.
func (a *Azure) listVMsSDK(ctx context.Context, scope provider.Node) ([]provider.VM, bool) {
	subID := scope.ID
	if scope.Kind == provider.KindResourceGroup {
		subID = scope.Meta["subscriptionId"]
	}
	if subID == "" {
		return nil, false
	}
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return nil, false
	}
	client, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, false
	}

	var vms []provider.VM
	if scope.Kind == provider.KindResourceGroup {
		pager := client.NewListPager(scope.Name, nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, false
			}
			vms = appendVMs(vms, page.Value)
		}
	} else {
		pager := client.NewListAllPager(nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, false
			}
			vms = appendVMs(vms, page.Value)
		}
	}
	return vms, true
}

func appendVMs(dst []provider.VM, page []*armcompute.VirtualMachine) []provider.VM {
	for _, v := range page {
		if v == nil || v.ID == nil {
			continue
		}
		id := *v.ID
		name := ""
		if v.Name != nil {
			name = *v.Name
		}
		location := ""
		if v.Location != nil {
			location = *v.Location
		}
		size := ""
		if v.Properties != nil && v.Properties.HardwareProfile != nil && v.Properties.HardwareProfile.VMSize != nil {
			size = string(*v.Properties.HardwareProfile.VMSize)
		}
		dst = append(dst, provider.VM{
			ID:       id,
			Name:     name,
			Type:     size,
			Location: location,
			Meta:     map[string]string{"resourceGroup": rgFromScope(id)},
		})
	}
	return dst
}

// vmShowSDK returns the raw ARM JSON for a VM. ok=false triggers the
// az vm show fallback.
func (a *Azure) vmShowSDK(ctx context.Context, id, subID string) ([]byte, bool) {
	if subID == "" {
		subID = subIDFromScope(id)
	}
	if subID == "" {
		return nil, false
	}
	rg := rgFromScope(id)
	name := vmNameFromID(id)
	if rg == "" || name == "" {
		return nil, false
	}
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return nil, false
	}
	client, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Get(ctx, rg, name, nil)
	if err != nil {
		return nil, false
	}
	raw, err := json.Marshal(resp.VirtualMachine)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// startVMSDK and stopVMSDK kick off the long-running start / deallocate
// operations. We don't wait on the LRO — the TUI already tells the user
// Azure is processing asynchronously.
func (a *Azure) startVMSDK(ctx context.Context, id, subID string) bool {
	client, rg, name, ok := a.vmClientFor(ctx, id, subID)
	if !ok {
		return false
	}
	_, err := client.BeginStart(ctx, rg, name, nil)
	return err == nil
}

func (a *Azure) stopVMSDK(ctx context.Context, id, subID string) bool {
	client, rg, name, ok := a.vmClientFor(ctx, id, subID)
	if !ok {
		return false
	}
	_, err := client.BeginDeallocate(ctx, rg, name, nil)
	return err == nil
}

func (a *Azure) vmClientFor(ctx context.Context, id, subID string) (*armcompute.VirtualMachinesClient, string, string, bool) {
	if subID == "" {
		subID = subIDFromScope(id)
	}
	if subID == "" {
		return nil, "", "", false
	}
	rg := rgFromScope(id)
	name := vmNameFromID(id)
	if rg == "" || name == "" {
		return nil, "", "", false
	}
	cred, err := a.subCredential(ctx, subID)
	if err != nil {
		return nil, "", "", false
	}
	client, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, "", "", false
	}
	return client, rg, name, true
}

// vmNameFromID pulls the VM name out of a /virtualMachines/<name> ARM id.
func vmNameFromID(id string) string {
	const marker = "/virtualmachines/"
	i := strings.Index(strings.ToLower(id), marker)
	if i < 0 {
		return ""
	}
	rest := id[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}
