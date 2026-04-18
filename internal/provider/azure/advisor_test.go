package azure

import "testing"

func TestParseRecommendations(t *testing.T) {
	data := []byte(`{
      "value": [
        {
          "properties": {
            "category": "Cost",
            "impact": "High",
            "impactedField": "Microsoft.Compute/virtualMachines",
            "impactedValue": "my-vm",
            "lastUpdated": "2026-04-10T10:00:00Z",
            "shortDescription": {
              "problem": "Right-size underutilized VMs",
              "solution": "Resize to a smaller SKU"
            }
          }
        },
        {
          "properties": {
            "category": "Security",
            "impact": "High",
            "impactedField": "Microsoft.Storage/storageAccounts",
            "impactedValue": "mystorage",
            "shortDescription": {
              "problem": "Secure transfer not enabled",
              "solution": "Enable secure transfer"
            }
          }
        }
      ]
    }`)
	recs, err := parseRecommendations(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("len=%d want 2", len(recs))
	}
	if recs[0].Category != "Cost" || recs[0].Impact != "High" {
		t.Errorf("[0]=%+v", recs[0])
	}
	if recs[0].ImpactedName != "my-vm" || recs[0].ImpactedType != "Microsoft.Compute/virtualMachines" {
		t.Errorf("[0] impact=%+v", recs[0])
	}
	if recs[1].Problem != "Secure transfer not enabled" {
		t.Errorf("[1].Problem=%q", recs[1].Problem)
	}
}

// TestParseRecommendationsResourceID verifies we extract the target resource
// id from either properties.resourceMetadata.resourceId or — when that's
// omitted — from the rec id prefix. The TUI's advisor filter uses this field
// to scope recommendations to the cursor row.
func TestParseRecommendationsResourceID(t *testing.T) {
	data := []byte(`{
      "value": [
        {
          "id": "/subscriptions/abc/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1/providers/Microsoft.Advisor/recommendations/rec1",
          "properties": {
            "category": "Cost",
            "impact": "Medium",
            "resourceMetadata": { "resourceId": "/subscriptions/abc/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1" },
            "shortDescription": { "problem": "p1", "solution": "s1" }
          }
        },
        {
          "id": "/subscriptions/abc/resourceGroups/rg-b/providers/Microsoft.Advisor/recommendations/rec2",
          "properties": {
            "category": "Security",
            "impact": "Low",
            "shortDescription": { "problem": "p2", "solution": "s2" }
          }
        }
      ]
    }`)
	recs, err := parseRecommendations(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := recs[0].ResourceID; got != "/subscriptions/abc/resourceGroups/rg-a/providers/Microsoft.Compute/virtualMachines/vm1" {
		t.Errorf("[0].ResourceID=%q", got)
	}
	if got := recs[1].ResourceID; got != "/subscriptions/abc/resourceGroups/rg-b" {
		t.Errorf("[1].ResourceID (derived from id) = %q", got)
	}
}
