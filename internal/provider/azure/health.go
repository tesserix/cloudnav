package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HealthStatus is the caller-facing classification returned to the TUI.
// Small and string-based so the UI layer can switch on it without
// importing azure internals.
const (
	HealthAvailable   = "Available"
	HealthDegraded    = "Degraded"
	HealthUnavailable = "Unavailable"
	HealthUnknown     = "Unknown"
)

// healthTTL bounds how long a per-subscription health snapshot stays fresh.
// Resource Health's own refresh interval is 1–2 minutes, so anything
// shorter than that is wasted work; anything much longer risks showing
// stale "Unavailable" on a resource that's already recovered.
const healthTTL = 2 * time.Minute

type subHealth struct {
	// ARN-lowercased resource ID → health classification.
	status  map[string]string
	fetched time.Time
	lastErr error
}

// ensureHealthCache returns a best-effort read of the health map for
// subID; callers get nil when Resource Health is unreachable for that sub
// and should render rows without the badge.
func (a *Azure) resourceHealth(ctx context.Context, subID string) map[string]string {
	if subID == "" {
		return nil
	}
	a.healthMu.Lock()
	h, ok := a.healthCache[subID]
	a.healthMu.Unlock()
	if ok && time.Since(h.fetched) < healthTTL {
		return h.status
	}

	status, err := a.fetchResourceHealth(ctx, subID)
	a.healthMu.Lock()
	if a.healthCache == nil {
		a.healthCache = map[string]*subHealth{}
	}
	a.healthCache[subID] = &subHealth{status: status, fetched: time.Now(), lastErr: err}
	a.healthMu.Unlock()
	return status
}

// fetchResourceHealth pulls the per-resource availability state for a sub
// in one ARM call. Classifier mirrors Azure's own portal terminology —
// Available / Degraded / Unavailable / Unknown — and we keep the map
// keys lowercased so lookups on mixed-case ARM ids still hit.
func (a *Azure) fetchResourceHealth(ctx context.Context, subID string) (map[string]string, error) {
	// api-version 2022-10-01 is the latest GA for availabilityStatuses
	// and accepts the subscription-scoped list endpoint — older versions
	// require a resource-level scope and paginate aggressively.
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.ResourceHealth/availabilityStatuses?api-version=2022-10-01",
		subID,
	)
	body, err := a.getJSONForSub(ctx, subID, url)
	if err != nil {
		return nil, err
	}
	return parseHealth(body)
}

func parseHealth(data []byte) (map[string]string, error) {
	var env struct {
		Value []struct {
			ID         string `json:"id"`
			Properties struct {
				AvailabilityState string `json:"availabilityState"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse resource health: %w", err)
	}
	out := make(map[string]string, len(env.Value))
	for _, v := range env.Value {
		// Each /availabilityStatuses/* id ends in the resource's own
		// scope; trim that suffix so the map is keyed on the plain
		// resource id used throughout the rest of the provider.
		id := strings.TrimSuffix(v.ID, "/providers/Microsoft.ResourceHealth/availabilityStatuses/current")
		out[strings.ToLower(id)] = classifyHealth(v.Properties.AvailabilityState)
	}
	return out, nil
}

// classifyHealth maps the raw Azure availability strings into the four
// status constants the TUI renders. "Available" is by far the majority
// state so we only colour the other three.
func classifyHealth(raw string) string {
	switch strings.ToLower(raw) {
	case "available":
		return HealthAvailable
	case "unavailable":
		return HealthUnavailable
	case "degraded":
		return HealthDegraded
	default:
		return HealthUnknown
	}
}
