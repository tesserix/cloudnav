package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fetchPolicyMaxes resolves the activation-hour cap for every unique
// (scope, roleDefinitionId) pair in parallel. Bounded concurrency keeps the
// call load polite; the overall timeout guarantees the PIM list never blocks
// on a slow or unauthorized scope. Entries with no data stay at zero and the
// TUI falls back to a sensible default.
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
// (in hours) for the given role at scope. Returns 0 when the policy is
// unreadable; callers treat that as "use default".
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

// maxHoursFromRules finds the "maximum duration" rule on an ARM PIM policy
// and converts it to hours. Other rule types (approval, justification) are
// ignored here.
func maxHoursFromRules(body []byte) int {
	var env struct {
		Properties struct {
			Rules []struct {
				ID         string `json:"id"`
				RuleType   string `json:"ruleType"`
				MaximumDur string `json:"maximumDuration"`
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
		lid := strings.ToLower(r.ID)
		if !strings.Contains(lid, "enablement_enduser_assignment") &&
			!strings.Contains(lid, "expiration_enduser_assignment") {
			continue
		}
		if h := parseISO8601Hours(r.MaximumDur); h > 0 {
			return h
		}
	}
	return 0
}

// parseISO8601Hours returns the hour count for simple ISO-8601 durations the
// PIM API emits: PT8H, PT30M, P1D, PT1H30M. Minutes round up to the next
// hour so callers always get a usable integer cap.
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
