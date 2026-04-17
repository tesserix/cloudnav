package azure

import "testing"

func TestParseCosts(t *testing.T) {
	data := []byte(`{
      "properties": {
        "columns": [
          {"name": "PreTaxCost", "type": "Number"},
          {"name": "ResourceGroupName", "type": "String"},
          {"name": "Currency", "type": "String"}
        ],
        "rows": [
          [49.6314894097288, "", "GBP"],
          [34.9618652175554, "acr-prod-rg", "GBP"],
          [0.83911255708965, "tiny-rg", "GBP"]
        ]
      }
    }`)
	costs, err := parseCosts(data)
	if err != nil {
		t.Fatal(err)
	}
	// Empty rg name rows should be skipped.
	if len(costs) != 2 {
		t.Fatalf("len=%d want 2", len(costs))
	}
	if got := costs["acr-prod-rg"]; got != "£34.96" {
		t.Errorf("acr-prod-rg=%q want £34.96", got)
	}
	if got := costs["tiny-rg"]; got != "£0.84" {
		t.Errorf("tiny-rg=%q want £0.84", got)
	}
}

func TestParseCostsMissingColumns(t *testing.T) {
	data := []byte(`{"properties":{"columns":[{"name":"Other"}],"rows":[]}}`)
	if _, err := parseCosts(data); err == nil {
		t.Error("expected error for missing cost/rg columns")
	}
}

func TestCurrencySymbol(t *testing.T) {
	cases := map[string]string{
		"USD": "$",
		"GBP": "£",
		"EUR": "€",
		"ZAR": "ZAR ",
		"":    " ",
	}
	for in, want := range cases {
		if got := currencySymbol(in); got != want {
			t.Errorf("currencySymbol(%q)=%q want %q", in, got, want)
		}
	}
}
