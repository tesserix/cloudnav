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
