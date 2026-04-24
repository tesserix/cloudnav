package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// subNameCache stores resolved subscription display names for IDs the
// user doesn't have direct access to (typical PIM case: eligible to
// activate into a sub, not in az account list). Populated via Resource
// Graph so one query covers any number of unknowns at once.
var (
	subNameMu    sync.RWMutex
	extraSubName = map[string]string{}
)

// resolveSubName returns the cached display name for a subscription id.
// Falls through to a.subName (the az-account-list-backed cache) first,
// then to the Resource-Graph-backed cache. Empty string means unknown.
func (a *Azure) resolveSubName(id string) string {
	if name := a.subName(id); name != "" {
		return name
	}
	subNameMu.RLock()
	defer subNameMu.RUnlock()
	return extraSubName[strings.ToLower(id)]
}

// learnSubNames hydrates the Resource-Graph-backed cache for the given
// subscription ids. Returns quietly on any failure — the PIM view falls
// back to raw ids, which is exactly what it does today.
func (a *Azure) learnSubNames(ctx context.Context, anchorSubID string, unknown []string) {
	if len(unknown) == 0 || anchorSubID == "" {
		return
	}
	// KQL deduplicated list — tolerate callers passing duplicates.
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(unknown))
	for _, id := range unknown {
		k := strings.ToLower(id)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		ids = append(ids, k)
	}

	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, "'"+kqlEscape(id)+"'")
	}
	kql := fmt.Sprintf(
		`resourcecontainers `+
			`| where type =~ 'microsoft.resources/subscriptions' `+
			`| where subscriptionId in~ (%s) `+
			`| project subscriptionId, name`,
		strings.Join(quoted, ","),
	)
	body := map[string]any{
		"subscriptions": []string{anchorSubID},
		"query":         kql,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	resp, err := a.doTenantRequest(ctx, anchorSubID, "POST",
		"https://management.azure.com/providers/Microsoft.ResourceGraph/resources?api-version="+resourceGraphAPIVersion,
		raw,
	)
	if err != nil {
		return
	}
	var page struct {
		Data []struct {
			SubscriptionID string `json:"subscriptionId"`
			Name           string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &page); err != nil {
		return
	}
	subNameMu.Lock()
	for _, row := range page.Data {
		if row.SubscriptionID == "" || row.Name == "" {
			continue
		}
		extraSubName[strings.ToLower(row.SubscriptionID)] = row.Name
	}
	subNameMu.Unlock()
}
