package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tesserix/cloudnav/internal/provider"
)

type gceJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Zone        string `json:"zone"`
	Status      string `json:"status"`
	MachineType string `json:"machineType"`
}

func (g *GCP) ListVMs(ctx context.Context, scope provider.Node) ([]provider.VM, error) {
	if scope.Kind != provider.KindProject {
		return nil, fmt.Errorf("gcp: vm list expects project scope, got %q", scope.Kind)
	}
	// SDK fast path — Compute Engine AggregatedList collapses every
	// zone into a single RPC. Falls through to the gcloud CLI when
	// ADC isn't available.
	if vms, sdkUsable, err := g.listVMsSDK(ctx, scope); sdkUsable && err == nil {
		return vms, nil
	}
	out, err := g.gcloud.Run(ctx,
		"compute", "instances", "list",
		"--project", scope.ID,
		"--format=json",
	)
	if err != nil {
		return nil, err
	}
	var items []gceJSON
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse gcloud compute instances list: %w", err)
	}
	vms := make([]provider.VM, 0, len(items))
	for _, v := range items {
		zone := tail(v.Zone, '/')
		mt := tail(v.MachineType, '/')
		vms = append(vms, provider.VM{
			ID:       v.Name,
			Name:     v.Name,
			State:    v.Status,
			Type:     mt,
			Location: zone,
			Meta: map[string]string{
				"project": scope.ID,
				"zone":    zone,
			},
		})
	}
	return vms, nil
}

func (g *GCP) ShowVM(ctx context.Context, id, scope string) ([]byte, error) {
	project, zone := splitGCEScope(scope)
	if project == "" || zone == "" {
		return nil, fmt.Errorf("gcp: vm show needs scope project/zone")
	}
	if data, sdkUsable, err := g.showVMSDK(ctx, id, project, zone); sdkUsable && err == nil {
		return data, nil
	}
	return g.gcloud.Run(ctx, "compute", "instances", "describe", id,
		"--project", project, "--zone", zone, "--format=json")
}

func (g *GCP) StartVM(ctx context.Context, id, scope string) error {
	project, zone := splitGCEScope(scope)
	if project == "" || zone == "" {
		return fmt.Errorf("gcp: vm start needs scope project/zone")
	}
	if sdkUsable, err := g.startVMSDK(ctx, id, project, zone); sdkUsable {
		return err
	}
	_, err := g.gcloud.Run(ctx, "compute", "instances", "start", id,
		"--project", project, "--zone", zone)
	return err
}

func (g *GCP) StopVM(ctx context.Context, id, scope string) error {
	project, zone := splitGCEScope(scope)
	if project == "" || zone == "" {
		return fmt.Errorf("gcp: vm stop needs scope project/zone")
	}
	if sdkUsable, err := g.stopVMSDK(ctx, id, project, zone); sdkUsable {
		return err
	}
	_, err := g.gcloud.Run(ctx, "compute", "instances", "stop", id,
		"--project", project, "--zone", zone)
	return err
}

func splitGCEScope(s string) (project, zone string) {
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func tail(s string, sep byte) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sep {
			return s[i+1:]
		}
	}
	return s
}
