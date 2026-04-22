package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tesserix/cloudnav/internal/provider"
)

// HealthEvents fans out across every subscription the caller can see and
// merges active Service Health events — service issues, planned
// maintenance, and health advisories — into a single list. Subs where
// the permission isn't granted are skipped silently; we never surface
// per-sub 403s because they'd drown out the actual incident data.
//
// The fanout uses the same concurrency cap as PIM so a 30-sub tenant
// doesn't open 30 simultaneous ARM connections.
func (a *Azure) HealthEvents(ctx context.Context) ([]provider.HealthEvent, error) {
	a.mu.RLock()
	subs := make([]string, 0, len(a.subs))
	for id := range a.subs {
		subs = append(subs, id)
	}
	a.mu.RUnlock()
	if len(subs) == 0 {
		// Prime the subscription cache on demand when the overlay is
		// opened before the user has entered the subs view.
		if _, err := a.Root(ctx); err != nil {
			return nil, err
		}
		a.mu.RLock()
		for id := range a.subs {
			subs = append(subs, id)
		}
		a.mu.RUnlock()
	}

	type result struct {
		events []provider.HealthEvent
	}
	out := make(chan result, len(subs))
	sem := make(chan struct{}, pimFanout)
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(sub string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			evs, _ := a.subHealthEvents(ctx, sub)
			out <- result{events: evs}
		}(sub)
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	seen := map[string]bool{}
	merged := []provider.HealthEvent{}
	for r := range out {
		for _, e := range r.events {
			key := e.ID + "|" + e.Scope
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, e)
		}
	}
	return merged, nil
}

// subHealthEvents reads the active service-health events list scoped to
// one subscription. We only return *active* events — resolved incidents
// are noise for the overlay's "what's broken right now?" framing.
func (a *Azure) subHealthEvents(ctx context.Context, subID string) ([]provider.HealthEvent, error) {
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.ResourceHealth/events?api-version=2023-10-01-preview&$filter=Properties/Status%%20eq%%20'Active'",
		subID,
	)
	body, err := a.getJSONForSub(ctx, subID, url)
	if err != nil {
		return nil, err
	}
	return parseHealthEvents(body, subID, a.subName(subID))
}

func parseHealthEvents(data []byte, subID, subName string) ([]provider.HealthEvent, error) {
	var env struct {
		Value []struct {
			Name       string `json:"name"`
			Properties struct {
				EventType   string `json:"eventType"`
				EventSource string `json:"eventSource"`
				Status      string `json:"status"`
				Title       string `json:"title"`
				Summary     string `json:"summary"`
				Level       string `json:"level"`
				StartTime   string `json:"impactStartTime"`
				Impact      []struct {
					ImpactedService string `json:"impactedService"`
					ImpactedRegions []struct {
						ImpactedRegion string `json:"impactedRegion"`
					} `json:"impactedRegions"`
				} `json:"impact"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse service health events: %w", err)
	}
	scopeLabel := subName
	if scopeLabel == "" {
		scopeLabel = shortTenant(subID)
	}
	out := make([]provider.HealthEvent, 0, len(env.Value))
	for _, v := range env.Value {
		level := healthLevelFromEventType(v.Properties.EventType)
		// Collapse multi-service / multi-region impact into a single
		// human-readable phrase so the overlay reads cleanly.
		service := ""
		regions := []string{}
		for _, imp := range v.Properties.Impact {
			if service == "" {
				service = imp.ImpactedService
			}
			for _, r := range imp.ImpactedRegions {
				if r.ImpactedRegion != "" {
					regions = append(regions, r.ImpactedRegion)
				}
			}
		}
		out = append(out, provider.HealthEvent{
			ID:        v.Name,
			Title:     v.Properties.Title,
			Level:     level,
			Status:    v.Properties.Status,
			Service:   service,
			Region:    strings.Join(regions, ", "),
			Scope:     scopeLabel,
			StartTime: v.Properties.StartTime,
			Summary:   v.Properties.Summary,
		})
	}
	return out, nil
}

// healthLevelFromEventType maps Azure's ServiceIssue / PlannedMaintenance
// / HealthAdvisory / Security enum to the normalised level string on
// provider.HealthEvent so the UI doesn't need to know Azure-specific
// terms.
func healthLevelFromEventType(t string) string {
	switch strings.ToLower(t) {
	case "serviceissue":
		return "incident"
	case "plannedmaintenance":
		return "maintenance"
	case "healthadvisory":
		return "advisory"
	case "security":
		return "security"
	default:
		return "incident"
	}
}

// Ensure Azure implements the HealthEventer interface at compile time.
var _ provider.HealthEventer = (*Azure)(nil)
