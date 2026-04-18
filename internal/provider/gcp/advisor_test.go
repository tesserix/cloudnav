package gcp

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

func TestParseRecommenderList(t *testing.T) {
	body := []byte(`[
      {
        "name":"projects/p1/locations/global/recommenders/google.compute.instance.MachineTypeRecommender/recommendations/rec-1",
        "description":"Consider resizing the machine type from n1-standard-8 to n1-standard-4",
        "lastRefreshTime":"2026-04-10T00:00:00Z",
        "priority":"P2",
        "primaryImpact":{"category":"COST"},
        "targetResources":["//compute.googleapis.com/projects/p1/zones/us-central1-a/instances/vm-foo"]
      },
      {
        "name":"projects/p1/locations/global/recommenders/google.compute.instance.IdleResourceRecommender/recommendations/rec-2",
        "description":"VM has been idle for 30 days",
        "priority":"P1",
        "primaryImpact":{"category":"COST"},
        "targetResources":["//compute.googleapis.com/projects/p1/zones/us-central1-a/instances/vm-idle"]
      }
    ]`)
	recs, err := parseRecommenderList(body, "Cost", "compute.Instance")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("len=%d want 2", len(recs))
	}
	if recs[0].Category != "Cost" {
		t.Errorf("[0].Category = %q", recs[0].Category)
	}
	if recs[0].Impact != "Medium" {
		t.Errorf("[0].Impact = %q, want Medium (from P2)", recs[0].Impact)
	}
	if recs[1].Impact != "High" {
		t.Errorf("[1].Impact = %q, want High (from P1)", recs[1].Impact)
	}
	if recs[0].ResourceID == "" {
		t.Error("ResourceID not populated")
	}
	if recs[0].ImpactedName != "vm-foo" {
		t.Errorf("ImpactedName = %q, want vm-foo (short name)", recs[0].ImpactedName)
	}
	if recs[0].ImpactedType != "compute.Instance" {
		t.Errorf("ImpactedType = %q", recs[0].ImpactedType)
	}
	if recs[0].LastUpdated != "2026-04-10T00:00:00Z" {
		t.Errorf("LastUpdated = %q", recs[0].LastUpdated)
	}
}

func TestParseRecommenderListEmpty(t *testing.T) {
	recs, err := parseRecommenderList([]byte(`[]`), "Cost", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("len=%d want 0", len(recs))
	}
}

func TestPriorityToImpact(t *testing.T) {
	cases := map[string]string{
		"P1": "High",
		"P2": "Medium",
		"P3": "Low",
		"P4": "Low",
		"":   "Low",
		"xx": "Low",
	}
	for in, want := range cases {
		if got := priorityToImpact(in); got != want {
			t.Errorf("priorityToImpact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleize(t *testing.T) {
	cases := map[string]string{
		"COST":        "Cost",
		"PERFORMANCE": "Performance",
		"":            "",
		"Security":    "Security",
	}
	for in, want := range cases {
		if got := titleize(in); got != want {
			t.Errorf("titleize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGCPSatisfiesAdvisor(t *testing.T) {
	var _ provider.Advisor = (*GCP)(nil)
}
