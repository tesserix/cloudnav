package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Lock struct {
	Name  string
	Level string
	Scope string
	RG    string
}

func (a *Azure) ResourceGroupLocks(ctx context.Context, subID string) (map[string][]Lock, error) {
	out, err := a.az.Run(ctx, "lock", "list", "--subscription", subID, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("azure lock list: %w", err)
	}
	return parseLocks(out)
}

func parseLocks(data []byte) (map[string][]Lock, error) {
	var items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Level string `json:"level"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse locks: %w", err)
	}
	out := make(map[string][]Lock, len(items))
	for _, it := range items {
		rg := rgFromScope(it.ID)
		if rg == "" {
			continue
		}
		key := strings.ToLower(rg)
		out[key] = append(out[key], Lock{
			Name:  it.Name,
			Level: it.Level,
			Scope: it.ID,
			RG:    rg,
		})
	}
	return out, nil
}

func rgFromScope(scope string) string {
	const marker = "/resourceGroups/"
	i := strings.Index(strings.ToLower(scope), strings.ToLower(marker))
	if i < 0 {
		return ""
	}
	rest := scope[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func (a *Azure) CreateRGLock(ctx context.Context, subID, rg, name, level string) error {
	if level == "" {
		level = "CanNotDelete"
	}
	if name == "" {
		name = "cloudnav-" + strings.ToLower(level)
	}
	_, err := a.az.Run(ctx,
		"lock", "create",
		"--name", name,
		"--lock-type", level,
		"--resource-group", rg,
		"--subscription", subID,
	)
	return err
}

func (a *Azure) DeleteRGLock(ctx context.Context, subID, rg, lockName string) error {
	_, err := a.az.Run(ctx,
		"lock", "delete",
		"--name", lockName,
		"--resource-group", rg,
		"--subscription", subID,
	)
	return err
}

func (a *Azure) DeleteResourceGroup(ctx context.Context, subID, rg string) error {
	_, err := a.az.Run(ctx,
		"group", "delete",
		"--name", rg,
		"--subscription", subID,
		"--yes", "--no-wait",
	)
	return err
}

// DeleteResource removes a single resource by its full ARM ID. Uses the
// ARM REST endpoint directly (no az-cli per-type shell-out) so one code
// path covers every resource kind — VMs, disks, IPs, NSGs, anything.
// The api-version query parameter is the trickiest bit: Azure requires
// a provider-specific version. We pick a recent compute/network/generic
// version based on the type namespace and fall back to a broad one for
// everything else.
func (a *Azure) DeleteResource(ctx context.Context, subID, resourceID, resourceType string) error {
	apiVer := apiVersionFor(resourceType)
	url := "https://management.azure.com" + resourceID + "?api-version=" + apiVer
	_, err := a.doTenantRequest(ctx, subID, "DELETE", url, nil)
	return err
}

// apiVersionFor returns a sensible api-version for a given ARM resource
// type. Values are the widely-supported stable versions as of 2024;
// Azure keeps older api-versions working for years, so a fixed pin
// here is safe even as newer ones ship.
func apiVersionFor(resourceType string) string {
	t := strings.ToLower(resourceType)
	switch {
	case strings.HasPrefix(t, "microsoft.compute/virtualmachines"):
		return "2024-03-01"
	case strings.HasPrefix(t, "microsoft.compute/disks"):
		return "2023-10-02"
	case strings.HasPrefix(t, "microsoft.compute/"):
		return "2024-03-01"
	case strings.HasPrefix(t, "microsoft.network/"):
		return "2024-01-01"
	case strings.HasPrefix(t, "microsoft.storage/"):
		return "2023-05-01"
	case strings.HasPrefix(t, "microsoft.sql/"):
		return "2023-08-01-preview"
	case strings.HasPrefix(t, "microsoft.web/"):
		return "2023-12-01"
	case strings.HasPrefix(t, "microsoft.containerservice/"):
		return "2024-02-01"
	case strings.HasPrefix(t, "microsoft.keyvault/"):
		return "2023-07-01"
	default:
		return "2021-04-01"
	}
}
