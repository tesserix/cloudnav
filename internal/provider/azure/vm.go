package azure

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

type vmJSON struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Location        string `json:"location"`
	ResourceGroup   string `json:"resourceGroup"`
	VMSize          string `json:"hardwareProfile.vmSize"`
	PowerState      string `json:"powerState"`
	HardwareProfile struct {
		VMSize string `json:"vmSize"`
	} `json:"hardwareProfile"`
}

func (a *Azure) ListVMs(ctx context.Context, scope provider.Node) ([]provider.VM, error) {
	args := []string{"vm", "list", "--show-details", "--output", "json"}
	switch scope.Kind {
	case provider.KindSubscription:
		args = append(args, "--subscription", scope.ID)
	case provider.KindResourceGroup:
		args = append(args, "--resource-group", scope.Name)
		if scope.Meta["subscriptionId"] != "" {
			args = append(args, "--subscription", scope.Meta["subscriptionId"])
		}
	default:
		return nil, fmt.Errorf("azure: vm list expects subscription or resource-group scope, got %q", scope.Kind)
	}
	out, err := a.az.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var items []vmJSON
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse az vm list: %w", err)
	}
	vms := make([]provider.VM, 0, len(items))
	for _, v := range items {
		vms = append(vms, provider.VM{
			ID:       v.ID,
			Name:     v.Name,
			State:    v.PowerState,
			Type:     v.HardwareProfile.VMSize,
			Location: v.Location,
			Meta: map[string]string{
				"resourceGroup": v.ResourceGroup,
			},
		})
	}
	return vms, nil
}

func (a *Azure) ShowVM(ctx context.Context, id, _ string) ([]byte, error) {
	return a.az.Run(ctx, "vm", "show", "--ids", id, "--output", "json")
}

func (a *Azure) StartVM(ctx context.Context, id, _ string) error {
	_, err := a.az.Run(ctx, "vm", "start", "--ids", id)
	return err
}

func (a *Azure) StopVM(ctx context.Context, id, _ string) error {
	_, err := a.az.Run(ctx, "vm", "deallocate", "--ids", id)
	return err
}
