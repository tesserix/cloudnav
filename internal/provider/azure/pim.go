// Package azure's PIM layer is split across a few small files so each cloud
// surface (Azure resources, Entra directory roles, Groups) stays isolated:
//
//	pim.go         — the PIMer interface impl that orchestrates list+activate
//	pim_azure.go   — Azure resource RBAC eligibilities + activation
//	pim_entra.go   — Microsoft Entra directory-role PIM over Graph
//	pim_group.go   — PIM-for-Groups membership over Graph
//	pim_policy.go  — role-management-policy lookups (max activation hours)
//	pim_tokens.go  — tenant-scoped token acquisition for ARM & Graph
//	pim_http.go    — shared bearer-token HTTP helpers
//	pim_util.go    — GUID generator, scope helpers
//
// Keeping them small lets the next cloud (AWS Identity Center, GCP JIT) drop
// in as its own file without touching this one.
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// ListEligibleRoles merges PIM eligibilities across all three Azure surfaces
// (resources, Entra directory roles, Groups) keyed by tenant. The caller's
// bearer token is tenant-scoped, so we fan out per-tenant and dedupe.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	tenants, err := a.pimTenants(ctx)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	out := []provider.PIMRole{}
	seen := map[string]bool{}
	var lastErr error

	for tid := range tenants {
		for _, lister := range a.pimListers() {
			roles, err := lister(ctx, tid, client)
			if err != nil {
				if lastErr == nil {
					lastErr = err
				}
				continue
			}
			for _, r := range roles {
				if seen[r.ID] {
					continue
				}
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", lastErr)
	}
	return out, nil
}

// pimLister fetches eligibilities for a single tenant. Each PIM surface
// provides one — adding a fourth source is a one-line addition to pimListers.
type pimLister func(ctx context.Context, tenantID string, client *http.Client) ([]provider.PIMRole, error)

func (a *Azure) pimListers() []pimLister {
	return []pimLister{
		a.listAzurePIM,
		a.listEntraPIM,
		a.listGroupPIM,
	}
}

// pimTenants returns the set of tenant ids the caller has subscription access
// to. Subscriptions that share a tenant are coalesced.
func (a *Azure) pimTenants(ctx context.Context) (map[string]bool, error) {
	out, err := a.az.Run(ctx, "account", "list", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("list subscriptions for PIM: %w", err)
	}
	var subs []subJSON
	if err := json.Unmarshal(out, &subs); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	tenants := map[string]bool{}
	for _, s := range subs {
		if s.TenantID != "" {
			tenants[s.TenantID] = true
		}
	}
	return tenants, nil
}

// ActivateRole dispatches activation to the right surface based on role.Source.
func (a *Azure) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	switch role.Source {
	case "entra":
		return a.activateEntraRole(ctx, role, justification, durationHours)
	case "group":
		return a.activateGroupRole(ctx, role, justification, durationHours)
	default:
		return a.activateAzureRole(ctx, role, justification, durationHours)
	}
}
