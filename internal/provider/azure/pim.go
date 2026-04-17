package azure

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// ListEligibleRoles returns PIM-eligible role assignments across every tenant
// the caller has subscription access to. The eligibility API is scoped to the
// tenant that issued the bearer token, so we ask `az account get-access-token`
// for a token per tenant and fan the query out.
func (a *Azure) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
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

	eligURL := "https://management.azure.com/providers/Microsoft.Authorization/roleEligibilityScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	activeURL := "https://management.azure.com/providers/Microsoft.Authorization/roleAssignmentScheduleInstances?api-version=2020-10-01&$filter=asTarget()"
	allRoles := []provider.PIMRole{}
	seen := map[string]bool{}
	var lastErr error

	client := &http.Client{Timeout: 30 * time.Second}

	for tid := range tenants {
		token, err := a.tenantToken(ctx, tid)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := fetchWithToken(ctx, client, eligURL, token)
		if err != nil {
			lastErr = err
			continue
		}
		roles, perr := parsePIM(body)
		if perr != nil {
			lastErr = perr
			continue
		}
		activeBody, _ := fetchWithToken(ctx, client, activeURL, token)
		active := parseActiveAssignments(activeBody)

		unique := map[string]struct{ scope, roleDef string }{}
		for _, r := range roles {
			polKey := r.Scope + "|" + r.RoleDefinitionID
			if _, ok := unique[polKey]; !ok {
				unique[polKey] = struct{ scope, roleDef string }{r.Scope, r.RoleDefinitionID}
			}
		}
		policyMax := fetchPolicyMaxes(ctx, client, token, unique)

		for _, r := range roles {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			r.TenantID = tid
			if name := a.subName(subIDFromScope(r.Scope)); name != "" {
				r.ScopeName = name
			}
			key := strings.ToLower(r.RoleDefinitionID + "|" + r.Scope)
			if until, ok := active[key]; ok {
				r.Active = true
				r.ActiveUntil = until
			}
			r.MaxDurationHours = policyMax[r.Scope+"|"+r.RoleDefinitionID]
			allRoles = append(allRoles, r)
		}
	}
	if len(allRoles) == 0 && lastErr != nil {
		return nil, fmt.Errorf("list eligible PIM roles: %w", lastErr)
	}
	return allRoles, nil
}

func parseActiveAssignments(body []byte) map[string]string {
	out := map[string]string{}
	if len(body) == 0 {
		return out
	}
	var env struct {
		Value []struct {
			Properties struct {
				Scope              string `json:"scope"`
				RoleDefinitionID   string `json:"roleDefinitionId"`
				EndDateTime        string `json:"endDateTime"`
				ExpandedProperties struct {
					RoleDefinition struct {
						ID string `json:"id"`
					} `json:"roleDefinition"`
				} `json:"expandedProperties"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return out
	}
	for _, v := range env.Value {
		roleDef := v.Properties.RoleDefinitionID
		if roleDef == "" {
			roleDef = v.Properties.ExpandedProperties.RoleDefinition.ID
		}
		if roleDef == "" || v.Properties.Scope == "" {
			continue
		}
		out[strings.ToLower(roleDef+"|"+v.Properties.Scope)] = v.Properties.EndDateTime
	}
	return out
}

func (a *Azure) tenantToken(ctx context.Context, tenantID string) (string, error) {
	out, err := a.az.Run(ctx,
		"account", "get-access-token",
		"--tenant", tenantID,
		"--resource", "https://management.azure.com/",
		"-o", "json",
	)
	if err != nil {
		return "", err
	}
	var t struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(out, &t); err != nil {
		return "", err
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("empty token for tenant %s", tenantID)
	}
	return t.AccessToken, nil
}

func fetchWithToken(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func parsePIM(data []byte) ([]provider.PIMRole, error) {
	var envelope struct {
		Value []struct {
			ID         string `json:"id"`
			Properties struct {
				Scope                           string `json:"scope"`
				PrincipalID                     string `json:"principalId"`
				RoleDefinitionID                string `json:"roleDefinitionId"`
				EndDateTime                     string `json:"endDateTime"`
				RoleEligibilityScheduleID       string `json:"roleEligibilityScheduleId"`
				LinkedRoleEligibilityScheduleID string `json:"linkedRoleEligibilityScheduleId"`
				ExpandedProperties              struct {
					RoleDefinition struct {
						DisplayName string `json:"displayName"`
						ID          string `json:"id"`
					} `json:"roleDefinition"`
				} `json:"expandedProperties"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse PIM response: %w", err)
	}
	roles := make([]provider.PIMRole, 0, len(envelope.Value))
	for _, v := range envelope.Value {
		roleDefID := v.Properties.RoleDefinitionID
		if roleDefID == "" {
			roleDefID = v.Properties.ExpandedProperties.RoleDefinition.ID
		}
		eligID := v.Properties.LinkedRoleEligibilityScheduleID
		if eligID == "" {
			eligID = v.Properties.RoleEligibilityScheduleID
		}
		roles = append(roles, provider.PIMRole{
			ID:               v.ID,
			RoleName:         v.Properties.ExpandedProperties.RoleDefinition.DisplayName,
			Scope:            v.Properties.Scope,
			PrincipalID:      v.Properties.PrincipalID,
			RoleDefinitionID: roleDefID,
			EligibilityID:    eligID,
			EndDateTime:      v.Properties.EndDateTime,
		})
	}
	return roles, nil
}

// ActivateRole requests activation of an eligible PIM role. durationHours is
// the requested elevation window (capped by the role's PIM policy).
func (a *Azure) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, durationHours int) error {
	if justification == "" {
		return fmt.Errorf("justification is required by PIM policy")
	}
	if role.Scope == "" || role.PrincipalID == "" || role.RoleDefinitionID == "" {
		return fmt.Errorf("PIM role is missing scope / principalId / roleDefinitionId")
	}
	if durationHours <= 0 {
		durationHours = 1
	}
	guid, err := newGUID()
	if err != nil {
		return err
	}
	body := map[string]any{
		"properties": map[string]any{
			"principalId":                     role.PrincipalID,
			"roleDefinitionId":                role.RoleDefinitionID,
			"requestType":                     "SelfActivate",
			"linkedRoleEligibilityScheduleId": role.EligibilityID,
			"justification":                   justification,
			"scheduleInfo": map[string]any{
				"expiration": map[string]any{
					"type":     "AfterDuration",
					"duration": fmt.Sprintf("PT%dH", durationHours),
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf(
		"https://management.azure.com%s/providers/Microsoft.Authorization/roleAssignmentScheduleRequests/%s?api-version=2020-10-01",
		role.Scope, guid,
	)
	_, err = a.az.Run(ctx,
		"rest",
		"--method", "PUT",
		"--url", url,
		"--body", string(raw),
	)
	return err
}

// fetchPolicyMaxes resolves the activation-hour cap for every unique
// (scope, roleDefinitionId) pair in parallel. Bounded concurrency keeps the
// call load polite; the per-call timeout guarantees the PIM list never blocks
// on a slow or unauthorized scope. Entries with no data stay at zero and fall
// back to the TUI's default.
func fetchPolicyMaxes(ctx context.Context, client *http.Client, token string, pairs map[string]struct{ scope, roleDef string }) map[string]int {
	out := map[string]int{}
	if len(pairs) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 12)
	for k, v := range pairs {
		wg.Add(1)
		sem <- struct{}{}
		go func(key, scope, roleDef string) {
			defer wg.Done()
			defer func() { <-sem }()
			h := fetchMaxActivationHours(ctx, client, scope, roleDef, token)
			mu.Lock()
			out[key] = h
			mu.Unlock()
		}(k, v.scope, v.roleDef)
	}
	wg.Wait()
	return out
}

// fetchMaxActivationHours returns the policy-defined max activation duration
// (in hours) for the given role at scope. Returns 0 if the policy can't be
// read or doesn't express a limit; callers fall back to a sensible default.
func fetchMaxActivationHours(ctx context.Context, client *http.Client, scope, roleDefID, token string) int {
	listURL := fmt.Sprintf(
		"https://management.azure.com%s/providers/Microsoft.Authorization/roleManagementPolicyAssignments?api-version=2020-10-01",
		scope,
	)
	body, err := fetchWithToken(ctx, client, listURL, token)
	if err != nil {
		return 0
	}
	var env struct {
		Value []struct {
			Properties struct {
				PolicyID         string `json:"policyId"`
				RoleDefinitionID string `json:"roleDefinitionId"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0
	}
	wantRole := strings.ToLower(roleDefID)
	for _, v := range env.Value {
		if v.Properties.PolicyID == "" {
			continue
		}
		if wantRole != "" && strings.ToLower(v.Properties.RoleDefinitionID) != wantRole {
			continue
		}
		if h := fetchMaxFromPolicy(ctx, client, v.Properties.PolicyID, token); h > 0 {
			return h
		}
	}
	return 0
}

func fetchMaxFromPolicy(ctx context.Context, client *http.Client, policyID, token string) int {
	url := fmt.Sprintf("https://management.azure.com%s?api-version=2020-10-01", policyID)
	body, err := fetchWithToken(ctx, client, url, token)
	if err != nil {
		return 0
	}
	return maxHoursFromRules(body)
}

func maxHoursFromRules(body []byte) int {
	var env struct {
		Properties struct {
			Rules []struct {
				ID            string `json:"id"`
				RuleType      string `json:"ruleType"`
				MaximumDur    string `json:"maximumDuration"`
				IsExpirationR bool   `json:"isExpirationRequired"`
			} `json:"rules"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0
	}
	for _, r := range env.Properties.Rules {
		if r.RuleType != "RoleManagementPolicyExpirationRule" {
			continue
		}
		if !strings.Contains(strings.ToLower(r.ID), "enablement_enduser_assignment") &&
			!strings.Contains(strings.ToLower(r.ID), "expiration_enduser_assignment") {
			continue
		}
		if h := parseISO8601Hours(r.MaximumDur); h > 0 {
			return h
		}
	}
	return 0
}

// parseISO8601Hours returns the hour count for simple ISO-8601 durations the
// PIM API emits: PT8H, PT30M, P1D, PT1H30M. Minutes round up to the next hour
// so callers always get a usable integer cap.
func parseISO8601Hours(d string) int {
	if d == "" || !strings.HasPrefix(d, "P") {
		return 0
	}
	s := d[1:]
	days := 0
	if i := strings.Index(s, "D"); i >= 0 {
		days, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}
	hours, minutes := 0, 0
	if strings.HasPrefix(s, "T") {
		s = s[1:]
		if i := strings.Index(s, "H"); i >= 0 {
			hours, _ = strconv.Atoi(s[:i])
			s = s[i+1:]
		}
		if i := strings.Index(s, "M"); i >= 0 {
			minutes, _ = strconv.Atoi(s[:i])
		}
	}
	total := days*24 + hours
	if minutes > 0 {
		total++
	}
	return total
}

// newGUID returns a random RFC 4122 v4 UUID without pulling in a dep.
func newGUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	), nil
}
