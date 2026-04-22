package azure

import (
	"strings"
	"testing"
)

func TestClassifyHealth(t *testing.T) {
	cases := map[string]string{
		"Available":   HealthAvailable,
		"available":   HealthAvailable,
		"Unavailable": HealthUnavailable,
		"Degraded":    HealthDegraded,
		"Unknown":     HealthUnknown,
		"":            HealthUnknown,
		"weird":       HealthUnknown,
	}
	for in, want := range cases {
		if got := classifyHealth(in); got != want {
			t.Errorf("classifyHealth(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseHealthTrimsAvailabilityStatusesSuffix(t *testing.T) {
	data := []byte(`{
		"value": [
			{"id":"/subscriptions/ABC/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1/providers/Microsoft.ResourceHealth/availabilityStatuses/current","properties":{"availabilityState":"Available"}},
			{"id":"/subscriptions/ABC/resourceGroups/rg-a/providers/Microsoft.Storage/storageAccounts/sa1/providers/Microsoft.ResourceHealth/availabilityStatuses/current","properties":{"availabilityState":"Degraded"}}
		]
	}`)
	got, err := parseHealth(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	// Keys are lowercased for mixed-case ARN matching.
	vmKey := strings.ToLower("/subscriptions/ABC/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1")
	if got[vmKey] != HealthAvailable {
		t.Errorf("vm status = %q, want Available", got[vmKey])
	}
	saKey := strings.ToLower("/subscriptions/ABC/resourceGroups/rg-a/providers/Microsoft.Storage/storageAccounts/sa1")
	if got[saKey] != HealthDegraded {
		t.Errorf("sa status = %q, want Degraded", got[saKey])
	}
	// The suffix should not remain in any key.
	for k := range got {
		if strings.Contains(k, "/availabilitystatuses/") {
			t.Errorf("key still contains availabilityStatuses suffix: %q", k)
		}
	}
}

func TestParseHealthEmpty(t *testing.T) {
	got, err := parseHealth([]byte(`{"value":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestHealthLevelFromEventType(t *testing.T) {
	cases := map[string]string{
		"ServiceIssue":       "incident",
		"serviceissue":       "incident",
		"PlannedMaintenance": "maintenance",
		"HealthAdvisory":     "advisory",
		"Security":           "security",
		"":                   "incident",
		"unknown":            "incident",
	}
	for in, want := range cases {
		if got := healthLevelFromEventType(in); got != want {
			t.Errorf("healthLevelFromEventType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseHealthEventsCollapsesImpact(t *testing.T) {
	// Multiple impactedRegions under a single event should concatenate
	// into a single comma-separated Region string so the overlay renders
	// one row per incident rather than one per region.
	data := []byte(`{
		"value":[
			{"name":"evt-1","properties":{"eventType":"ServiceIssue","status":"Active","title":"SQL outage","level":"Warning","impactStartTime":"2026-04-21T10:00:00Z","impact":[{"impactedService":"SQL Database","impactedRegions":[{"impactedRegion":"eastus"},{"impactedRegion":"eastus2"}]}]}},
			{"name":"evt-2","properties":{"eventType":"PlannedMaintenance","status":"Active","title":"Storage maintenance","level":"Warning","impactStartTime":"2026-04-22T09:00:00Z","impact":[{"impactedService":"Storage","impactedRegions":[{"impactedRegion":"westeurope"}]}]}}
		]
	}`)
	events, err := parseHealthEvents(data, "sub-1", "Prod Sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Region != "eastus, eastus2" {
		t.Errorf("regions merged = %q", events[0].Region)
	}
	if events[0].Level != "incident" {
		t.Errorf("level = %q, want incident", events[0].Level)
	}
	if events[1].Level != "maintenance" {
		t.Errorf("level = %q, want maintenance", events[1].Level)
	}
	if events[0].Scope != "Prod Sub" {
		t.Errorf("scope = %q, want %q", events[0].Scope, "Prod Sub")
	}
}
