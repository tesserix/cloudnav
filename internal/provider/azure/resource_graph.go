package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Azure Resource Graph lets us query thousands of resources across
// subscriptions in a single KQL call. Replaces the N-sequential per-RG
// `az resource list` fanout used by aggregated and cross-sub views.
//
// Endpoint doc: https://learn.microsoft.com/en-us/rest/api/azureresourcegraph/resourcegraph/resources/resources

const resourceGraphAPIVersion = "2022-10-01"

// ResourcesInRGs returns resources for the given (subID, rgName) pairs
// in a single Resource Graph KQL query. Intended for the "selected N
// resource groups, show me their resources" aggregated path — one HTTP
// call instead of N.
//
// On failure it returns an error; callers that want the slow per-RG
// fallback can catch it and walk rgs via Children() instead.
func (a *Azure) ResourcesInRGs(ctx context.Context, subID string, rgNames []string) ([]provider.Node, error) {
	if len(rgNames) == 0 {
		return nil, nil
	}
	subIDs := []string{subID}
	// Build: resourceGroup in~ ('rg-a','rg-b', ...)
	quoted := make([]string, 0, len(rgNames))
	for _, rg := range rgNames {
		quoted = append(quoted, "'"+kqlEscape(rg)+"'")
	}
	q := fmt.Sprintf(
		`Resources | where resourceGroup in~ (%s) `+
			`| project id, name, type, location, resourceGroup, subscriptionId, tags, `+
			`createdTime = todatetime(properties.createdTime), `+
			`changedTime = todatetime(properties.changedTime) `+
			`| order by name asc`,
		strings.Join(quoted, ","),
	)
	return a.queryResourceGraph(ctx, subID, subIDs, q)
}

// ResourcesAcrossSubs runs a KQL query across multiple subscriptions.
// Useful for search / palette drills that want "all resources whose name
// matches X across my accessible subs".
func (a *Azure) ResourcesAcrossSubs(ctx context.Context, subIDs []string, kql string) ([]provider.Node, error) {
	if len(subIDs) == 0 {
		return nil, nil
	}
	// Pick any sub for the tenant token. Resource Graph normalises
	// across tenants internally but the Bearer header is tenant-scoped,
	// so we use the first sub's home tenant.
	return a.queryResourceGraph(ctx, subIDs[0], subIDs, kql)
}

// rgResource is the minimal projection Resource Graph returns for each
// resource. Fields mirror the KQL `project` clause above; unknown or
// null values come back as zero-value strings.
type rgResource struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Location       string          `json:"location"`
	ResourceGroup  string          `json:"resourceGroup"`
	SubscriptionID string          `json:"subscriptionId"`
	Tags           json.RawMessage `json:"tags"`
	CreatedTime    string          `json:"createdTime"`
	ChangedTime    string          `json:"changedTime"`
}

type resourceGraphResponse struct {
	Data  []rgResource `json:"data"`
	Count int64        `json:"count"`
	// SkipToken carries pagination state when the result exceeds the
	// server-side page limit (default 1000). Callers that need the full
	// set should loop until this is empty.
	SkipToken string `json:"$skipToken,omitempty"`
}

func (a *Azure) queryResourceGraph(ctx context.Context, tokenSubID string, subIDs []string, kql string) ([]provider.Node, error) {
	url := "https://management.azure.com/providers/Microsoft.ResourceGraph/resources?api-version=" + resourceGraphAPIVersion
	var out []provider.Node
	skip := ""
	for {
		body := map[string]any{
			"subscriptions": subIDs,
			"query":         kql,
		}
		if skip != "" {
			body["options"] = map[string]any{"$skipToken": skip}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		resp, err := a.doTenantRequest(ctx, tokenSubID, "POST", url, raw)
		if err != nil {
			return nil, fmt.Errorf("resource graph query: %w", err)
		}
		var page resourceGraphResponse
		if err := json.Unmarshal(resp, &page); err != nil {
			return nil, fmt.Errorf("parse resource graph: %w", err)
		}
		for _, r := range page.Data {
			out = append(out, rgResourceToNode(r))
		}
		if page.SkipToken == "" {
			break
		}
		skip = page.SkipToken
	}
	return out, nil
}

func rgResourceToNode(r rgResource) provider.Node {
	meta := map[string]string{
		"type":           r.Type,
		"subscriptionId": r.SubscriptionID,
	}
	if r.CreatedTime != "" {
		meta["createdTime"] = r.CreatedTime
	}
	if r.ChangedTime != "" {
		meta["changedTime"] = r.ChangedTime
	}
	if tags := tagsFromRaw(r.Tags); tags != "" {
		meta["tags"] = tags
	}
	return provider.Node{
		ID:       r.ID,
		Name:     r.Name,
		Kind:     provider.KindResource,
		Location: r.Location,
		State:    shortType(r.Type),
		Meta:     meta,
	}
}

// tagsFromRaw renders the KQL tags bag (an object) as a compact
// "k1=v1,k2=v2" string, matching the existing formatTags contract.
func tagsFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Keep insertion order stable: sort so the tag string is
	// deterministic across calls (otherwise table rows flicker on
	// re-render).
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// kqlEscape doubles single quotes so a resource group name like "a'b"
// can't break the query or smuggle a second clause.
func kqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// sortStrings is a tiny wrapper so this file doesn't need to import
// the full sort package for one call.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
