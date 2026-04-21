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
