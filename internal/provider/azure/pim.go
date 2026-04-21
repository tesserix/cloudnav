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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// pimFanout caps concurrent per-(tenant × lister) HTTP requests. Azure Graph
// rate-limits aggressively on cold tokens, so we keep this modest; the
// parallelism still turns a 30-call sequential walk into a handful of waves.
const pimFanout = 8

// PIM source tags. These match the provider.PIMRole.Source strings the TUI
// switches on for its Azure / Entra / Groups tabs — kept as package-local
// constants so goconst doesn't flag the four-way-repeated literals and
// tests have a single place to pivot on.
const (
	pimSrcAzure = "azure"
	pimSrcEntra = "entra"
	pimSrcGroup = "group"
)

// ListEligibleRoles merges PIM eligibilities across all three Azure surfaces
// (resources, Entra directory roles, Groups) over every tenant the caller
// has visibility into — subscription-owning tenants *and* directory-only
// (guest / invitee) tenants where they may hold Entra or Groups roles.
//
// Per-tenant authentication failures (the common case of `az login --tenant
// X` never having been run) don't silently hide that tenant's roles any
// more; a diagnostic row is emitted so the user can see *why* roles are
// missing from CIVICADEV / Prod / etc.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	tenants, err := a.pimTenants(ctx)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}

	type listerEntry struct {
		source string // pimSrc{Azure,Entra,Group} for diagnostics
		fn     pimLister
	}
	listers := []listerEntry{
		{pimSrcAzure, a.listAzurePIM},
		{pimSrcEntra, a.listEntraPIM},
		{pimSrcGroup, a.listGroupPIM},
	}

	type tenantResult struct {
		tid    string
		source string
		roles  []provider.PIMRole
		err    error
	}

	// Fan out across tenants × listers so a user with N tenants doesn't pay
	// N sequential round-trips per source. A small semaphore protects Graph
	// rate limits.
	results := make(chan tenantResult, len(tenants)*len(listers))
	sem := make(chan struct{}, pimFanout)
	var wg sync.WaitGroup
	for tid := range tenants {
		for _, l := range listers {
			wg.Add(1)
			go func(tid, src string, fn pimLister) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				roles, err := fn(ctx, tid, client)
				results <- tenantResult{tid: tid, source: src, roles: roles, err: err}
			}(tid, l.source, l.fn)
		}
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	out := []provider.PIMRole{}
	seen := map[string]bool{}
	// Track which (tenant, source) pairs failed so we can emit one
	// diagnostic row per failing source rather than one per HTTP error
	// (which can include both Graph and policy-readback failures).
	failed := map[string]map[string]error{} // tid -> source -> first err
	succeeded := map[string]map[string]bool{}
	var lastErr error

	for r := range results {
		if r.err != nil {
			if lastErr == nil {
				lastErr = r.err
			}
			if failed[r.tid] == nil {
				failed[r.tid] = map[string]error{}
			}
			if _, ok := failed[r.tid][r.source]; !ok {
				failed[r.tid][r.source] = r.err
			}
			continue
		}
		if succeeded[r.tid] == nil {
			succeeded[r.tid] = map[string]bool{}
		}
		succeeded[r.tid][r.source] = true
		for _, role := range r.roles {
			if seen[role.ID] {
				continue
			}
			seen[role.ID] = true
			out = append(out, role)
		}
	}

	// Emit diagnostic rows only for source/tenant combos that fully failed —
	// i.e. not a single successful response came back. A transient fluke on
	// one call shouldn't scare users with a red row next to a healthy source.
	for tid, srcErrs := range failed {
		for src, srcErr := range srcErrs {
			if succeeded[tid][src] {
				continue
			}
			out = append(out, a.diagnosticRole(tid, src, srcErr))
		}
	}

	// Stable ordering so the list doesn't shuffle run-to-run. Azure first,
	// then Entra, then Groups — matching the surface tabs in the UI; within
	// each source, by role name.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return pimSourceOrder(out[i].Source) < pimSourceOrder(out[j].Source)
		}
		return out[i].RoleName < out[j].RoleName
	})

	if len(out) == 0 && lastErr != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", lastErr)
	}
	return out, nil
}

// pimLister fetches eligibilities for a single tenant. Each PIM surface
// provides one — adding a fourth source is a one-line addition to the
// listers table in ListEligibleRoles.
type pimLister func(ctx context.Context, tenantID string, client *http.Client) ([]provider.PIMRole, error)

// pimTenants returns every tenant the caller should be queried against.
// Subs-owning tenants *plus* tenants surfaced by the ARM /tenants endpoint,
// which includes directory-only memberships (guest / invitee) where the
// user can hold Entra or Groups PIM eligibilities without ever having a
// subscription there. The map value is the human-readable tenant name when
// known, for diagnostic rows.
func (a *Azure) pimTenants(ctx context.Context) (map[string]string, error) {
	tenants := map[string]string{}

	// 1) Subscription-owning tenants. We do this rather than calling
	// a.pimTenants before to tolerate fetchTenants failures.
	out, err := a.az.Run(ctx, "account", "list", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("list subscriptions for PIM: %w", err)
	}
	var subs []subJSON
	if err := json.Unmarshal(out, &subs); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	for _, s := range subs {
		if s.TenantID != "" {
			tenants[s.TenantID] = ""
		}
	}

	// 2) Directory-only tenants. fetchTenants populates a.tenants from
	// https://management.azure.com/tenants — that list includes tenants
	// where the user holds only directory roles. Run it on demand so PIM
	// works even if the subs view wasn't visited first.
	a.mu.RLock()
	known := a.tenants
	a.mu.RUnlock()
	if len(known) == 0 {
		a.fetchTenants(ctx)
		a.mu.RLock()
		known = a.tenants
		a.mu.RUnlock()
	}
	for tid, name := range known {
		if _, seen := tenants[tid]; !seen || name != "" {
			tenants[tid] = name
		}
	}

	return tenants, nil
}

// diagnosticRole builds a placeholder PIMRole rendered in the UI when a
// tenant's PIM source fully failed. We keep RoleDefinitionID empty so the
// activation code path can't accidentally target it, and put the tenant +
// actionable hint into the scope/name fields so the existing renderer
// surfaces them without any UI changes.
func (a *Azure) diagnosticRole(tid, source string, err error) provider.PIMRole {
	name := a.tenantName(tid)
	if name == "" {
		name = shortTenant(tid)
	}
	hint := "run: az login --tenant " + tid
	if !looksLikeAuthError(err) {
		hint = strings.TrimSpace(firstLine(err.Error()))
		if len(hint) > 80 {
			hint = hint[:77] + "..."
		}
	}
	return provider.PIMRole{
		ID:        "diag:" + source + ":" + tid,
		RoleName:  "⚠ cannot list " + sourceLabel(source) + " roles in " + name,
		Scope:     tid,
		ScopeName: hint,
		TenantID:  tid,
		Source:    source,
	}
}

// sourceLabel returns a short human tag for diagnostic messages. We keep
// this here (rather than referencing the TUI's pimSourceLabel) to avoid
// coupling the provider layer to the UI.
func sourceLabel(src string) string {
	switch src {
	case pimSrcEntra:
		return "Entra"
	case pimSrcGroup:
		return "Groups"
	case pimSrcAzure:
		return "Azure resources"
	default:
		return src
	}
}

// pimSourceOrder gives a stable sort key to each PIM surface so tabs line
// up with the UI (Azure → Entra → Groups).
func pimSourceOrder(src string) int {
	switch src {
	case pimSrcAzure, "":
		return 0
	case pimSrcEntra:
		return 1
	case pimSrcGroup:
		return 2
	}
	return 3
}

// looksLikeAuthError returns true when the underlying error reads like a
// missing-tenant-login problem (the common case). Used to tune the
// diagnostic hint toward the actionable remediation.
func looksLikeAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"no subscription found",
		"please run 'az login'",
		"interactive authentication",
		"aadsts50076",  // MFA required
		"aadsts50158",  // conditional access
		"aadsts700003", // tenant not found for account
		"refresh token",
		"token expired",
		"failed to get access token",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// firstLine returns the first newline-delimited line of a multi-line error
// so the diagnostic stays on one row.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// shortTenant renders a tenant id as "abc12345-…" when no name is known,
// so diagnostic rows stay readable.
func shortTenant(tid string) string {
	if len(tid) > 12 {
		return tid[:8] + "…"
	}
	return tid
}

// ActivateRole dispatches activation to the right surface based on role.Source.
func (a *Azure) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	// Refuse to activate a diagnostic stub — it has no role definition id
	// and would produce a cryptic 400 from PIM.
	if strings.HasPrefix(role.ID, "diag:") {
		return fmt.Errorf("this row is a diagnostic — run `az login --tenant %s` and reopen PIM", role.TenantID)
	}
	switch role.Source {
	case pimSrcEntra:
		return a.activateEntraRole(ctx, role, justification, durationHours)
	case pimSrcGroup:
		return a.activateGroupRole(ctx, role, justification, durationHours)
	default:
		return a.activateAzureRole(ctx, role, justification, durationHours)
	}
}
