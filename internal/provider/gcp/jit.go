package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tesserix/cloudnav/internal/provider"
)

// ListEligibleRoles fetches Privileged Access Manager (PAM) entitlements the
// caller is eligible for across every accessible project. PAM is Google's
// real PIM equivalent (GA 2024) — when it's not enabled on any project we
// fall back to a helpful message pointing at conditional IAM bindings.
//
// Each entitlement becomes a provider.PIMRole so the TUI renders them
// alongside Azure / Entra / Groups roles in the existing PIM overlay.
func (g *GCP) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	projects, err := g.listProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("gcp: no accessible projects")
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	out := []provider.PIMRole{}
	errs := []string{}
	for _, pid := range projects {
		pid := pid
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			roles, active, err := g.fetchPAMForProject(ctx, pid)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err.Error())
				return
			}
			for i := range roles {
				if until, ok := active[roles[i].ID]; ok {
					roles[i].Active = true
					roles[i].ActiveUntil = until
				}
			}
			out = append(out, roles...)
		}()
	}
	wg.Wait()

	if len(out) == 0 {
		return nil, fmt.Errorf("gcp: no PAM entitlements found — either Privileged Access Manager isn't enabled on any of your projects, or use conditional IAM:\n  gcloud projects add-iam-policy-binding <PROJECT> --member=user:... --role=roles/<ROLE> --condition='expression=request.time < timestamp(...)'")
	}
	return out, nil
}

func (g *GCP) listProjectIDs(ctx context.Context) ([]string, error) {
	out, err := g.gcloud.Run(ctx, "projects", "list", "--format=value(projectId)")
	if err != nil {
		return nil, fmt.Errorf("gcp list projects: %w", err)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// fetchPAMForProject returns the eligible entitlements for one project plus
// a map of (entitlement name → active grant expiry) derived from currently-
// ACTIVE grants, so the TUI can flip the ACTIVE badge without a second
// round trip.
func (g *GCP) fetchPAMForProject(ctx context.Context, projectID string) ([]provider.PIMRole, map[string]string, error) {
	listOut, err := g.gcloud.Run(ctx,
		"beta", "pam", "entitlements", "list",
		"--project="+projectID,
		"--location=global",
		"--format=json",
	)
	if err != nil {
		if isPAMNotEnabled(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("pam entitlements list (%s): %w", projectID, err)
	}
	roles, err := parsePAMEntitlements(listOut, projectID)
	if err != nil {
		return nil, nil, err
	}

	// Best-effort active grants fetch.
	grantsOut, _ := g.gcloud.Run(ctx,
		"beta", "pam", "grants", "list",
		"--project="+projectID,
		"--location=global",
		"--format=json",
	)
	active := parsePAMActiveGrants(grantsOut)
	return roles, active, nil
}

// isPAMNotEnabled detects the common "API not enabled" error shape so we
// can skip projects silently instead of surfacing 404s.
func isPAMNotEnabled(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "privilegedaccessmanager") &&
		(strings.Contains(s, "not been used") ||
			strings.Contains(s, "is disabled") ||
			strings.Contains(s, "not enabled"))
}

func parsePAMEntitlements(data []byte, projectID string) ([]provider.PIMRole, error) {
	var items []struct {
		Name               string `json:"name"`
		MaxRequestDuration string `json:"maxRequestDuration"`
		PrivilegedAccess   struct {
			GcpIAMAccess struct {
				RoleBindings []struct {
					Role string `json:"role"`
				} `json:"roleBindings"`
				Resource string `json:"resource"`
			} `json:"gcpIamAccess"`
		} `json:"privilegedAccess"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse pam entitlements: %w", err)
	}
	out := make([]provider.PIMRole, 0, len(items))
	for _, it := range items {
		roleName := "access"
		if len(it.PrivilegedAccess.GcpIAMAccess.RoleBindings) > 0 {
			roleName = it.PrivilegedAccess.GcpIAMAccess.RoleBindings[0].Role
		}
		scope := it.PrivilegedAccess.GcpIAMAccess.Resource
		if scope == "" {
			scope = "projects/" + projectID
		}
		out = append(out, provider.PIMRole{
			ID:               it.Name,
			RoleName:         roleName,
			Scope:            scope,
			ScopeName:        projectID,
			TenantID:         projectID,
			RoleDefinitionID: shortName(it.Name),
			EligibilityID:    it.Name,
			MaxDurationHours: parsePAMDurationHours(it.MaxRequestDuration),
			Source:           "gcp-pam",
		})
	}
	return out, nil
}

// parsePAMDurationHours converts a gRPC duration like "3600s" (1h) or "28800s"
// (8h) into an hour count. Returns 0 when unparseable.
func parsePAMDurationHours(d string) int {
	if d == "" {
		return 0
	}
	d = strings.TrimSuffix(d, "s")
	var secs int
	if _, err := fmt.Sscanf(d, "%d", &secs); err != nil || secs <= 0 {
		return 0
	}
	h := secs / 3600
	if secs%3600 != 0 {
		h++
	}
	return h
}

// parsePAMActiveGrants returns a map of entitlementId → expiry for grants
// currently in ACTIVE state.
func parsePAMActiveGrants(data []byte) map[string]string {
	out := map[string]string{}
	if len(data) == 0 {
		return out
	}
	var items []struct {
		Name       string `json:"name"` // .../entitlements/E/grants/G
		State      string `json:"state"`
		ExpireTime string `json:"expireTime"`
		Parent     string `json:"entitlement"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return out
	}
	for _, it := range items {
		if strings.ToUpper(it.State) != "ACTIVE" {
			continue
		}
		entID := it.Parent
		if entID == "" {
			// Derive from the grant name: strip "/grants/<g>"
			if i := strings.Index(it.Name, "/grants/"); i > 0 {
				entID = it.Name[:i]
			}
		}
		if entID != "" {
			out[entID] = it.ExpireTime
		}
	}
	return out
}

// ActivateRole creates a PAM grant request against the entitlement. Matches
// the Azure activation UX: justification + duration, async-eventual.
func (g *GCP) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if role.Source != "gcp-pam" {
		return errors.New("gcp: only Privileged Access Manager roles can be activated here")
	}
	if role.EligibilityID == "" {
		return errors.New("gcp: missing entitlement id on role")
	}
	if justification == "" {
		return errors.New("gcp: justification is required")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	_, err := g.gcloud.Run(ctx,
		"beta", "pam", "grants", "create",
		"--entitlement="+role.EligibilityID,
		"--justification="+justification,
		fmt.Sprintf("--requested-duration=%ds", durationHours*3600),
		"--format=json",
	)
	if err != nil {
		return fmt.Errorf("gcp grant create: %w", err)
	}
	return nil
}
