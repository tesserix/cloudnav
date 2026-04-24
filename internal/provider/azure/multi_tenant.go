package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// allTenants returns every tenant id the caller might have access to,
// merging three independent sources so we catch cross-tenant guest
// memberships that a single source misses:
//
//  1. `az account tenant list` — what the portal uses for its
//     directories dropdown. Includes tenants the user is a member of
//     even when they don't own any subscription there.
//  2. ARM /tenants endpoint — tenants discoverable via the current
//     SDK-minted ARM token.
//  3. /subscriptions pager — every subscription's home tenant.
//
// Value is the tenant display name when known, empty otherwise.
func (a *Azure) allTenants(ctx context.Context) map[string]string {
	out := map[string]string{}

	// 1. az account tenant list — tolerate failure silently; we still
	// have sources 2 and 3 below.
	if raw, err := a.az.Run(ctx, "account", "tenant", "list", "-o", "json"); err == nil {
		var tl []struct {
			TenantID    string `json:"tenantId"`
			DisplayName string `json:"displayName"`
		}
		if err := json.Unmarshal(raw, &tl); err == nil {
			for _, t := range tl {
				if t.TenantID != "" {
					out[t.TenantID] = t.DisplayName
				}
			}
		}
	}

	// 2. ARM /tenants via SDK token.
	if m, ok := a.fetchTenantsSDK(ctx); ok {
		for tid, name := range m {
			if name != "" || out[tid] == "" {
				out[tid] = name
			}
		}
	}

	// 3. Tenant ids implied by the subs the caller can see.
	if subs, err := a.listSubscriptionsSDK(ctx); err == nil {
		for _, s := range subs {
			if tid := s.Meta["tenantId"]; tid != "" {
				if _, seen := out[tid]; !seen {
					out[tid] = ""
				}
			}
		}
	}

	return out
}

// listSubscriptionsMultiTenant fans out a per-tenant /subscriptions call
// so subs from tenants the default credential doesn't cover (cross-tenant
// guest memberships) still show up. Mints one tenant-scoped token per
// tenant in parallel.
func (a *Azure) listSubscriptionsMultiTenant(ctx context.Context) ([]subJSON, error) {
	tenants := a.allTenants(ctx)
	if len(tenants) == 0 {
		return nil, fmt.Errorf("no tenants discovered")
	}

	type result struct {
		subs []subJSON
	}
	results := make(chan result, len(tenants))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for tid := range tenants {
		tid := tid
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			subs, err := a.subsForTenant(ctx, tid)
			if err != nil {
				return
			}
			results <- result{subs: subs}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	// Dedupe by subscription id — a sub can be reachable via more than
	// one tenant credential for guest accounts.
	seen := map[string]struct{}{}
	var out []subJSON
	for r := range results {
		for _, s := range r.subs {
			if _, ok := seen[s.ID]; ok {
				continue
			}
			seen[s.ID] = struct{}{}
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no subscriptions visible in any tenant")
	}
	return out, nil
}

// subsForTenant queries ARM /subscriptions with a token scoped to the
// given tenant. Returns the subs whose home tenant matches.
func (a *Azure) subsForTenant(ctx context.Context, tenantID string) ([]subJSON, error) {
	token, err := a.tenantTokenFor(ctx, tenantID, armResource)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://management.azure.com/subscriptions?api-version=2022-12-01", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var env struct {
		Value []struct {
			SubscriptionID string `json:"subscriptionId"`
			DisplayName    string `json:"displayName"`
			State          string `json:"state"`
			TenantID       string `json:"tenantId"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}
	out := make([]subJSON, 0, len(env.Value))
	for _, s := range env.Value {
		if s.SubscriptionID == "" {
			continue
		}
		tid := s.TenantID
		if tid == "" {
			tid = tenantID
		}
		out = append(out, subJSON{
			ID:       s.SubscriptionID,
			Name:     s.DisplayName,
			State:    s.State,
			TenantID: tid,
		})
	}
	return out, nil
}
