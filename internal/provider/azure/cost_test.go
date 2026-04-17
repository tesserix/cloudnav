package azure

import (
	"strings"
	"testing"
	"time"
)

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

func TestFormatCostWithDelta(t *testing.T) {
	cases := []struct {
		name         string
		cur, last    costCell
		wantContains []string
		wantExact    string
	}{
		{
			name:         "up by 12%",
			cur:          costCell{amount: 112, currency: "GBP"},
			last:         costCell{amount: 100, currency: "GBP"},
			wantContains: []string{"£112.00", "↑", "12%"},
		},
		{
			name:         "down by 10%",
			cur:          costCell{amount: 90, currency: "GBP"},
			last:         costCell{amount: 100, currency: "GBP"},
			wantContains: []string{"£90.00", "↓", "10%"},
		},
		{
			name:         "flat within threshold",
			cur:          costCell{amount: 101, currency: "GBP"},
			last:         costCell{amount: 100, currency: "GBP"},
			wantContains: []string{"£101.00", "→"},
		},
		{
			name:         "new this month",
			cur:          costCell{amount: 50, currency: "GBP"},
			last:         costCell{amount: 0, currency: "GBP"},
			wantContains: []string{"£50.00", "new"},
		},
		{
			name:      "both zero",
			cur:       costCell{amount: 0, currency: "GBP"},
			last:      costCell{amount: 0, currency: "GBP"},
			wantExact: "£0.00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatCostWithDelta(tc.cur, tc.last)
			if tc.wantExact != "" {
				if got != tc.wantExact {
					t.Errorf("got %q want %q", got, tc.wantExact)
				}
				return
			}
			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("got %q, want it to contain %q", got, sub)
				}
			}
		})
	}
}

func TestLastMonthSamePeriod(t *testing.T) {
	// Mid-month
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	from, to := lastMonthSamePeriod(now)
	if from.Month() != time.March || from.Day() != 1 {
		t.Errorf("from=%v", from)
	}
	if to.Month() != time.March || to.Day() != 17 {
		t.Errorf("to=%v", to)
	}
}

func TestLastMonthSamePeriodClamp(t *testing.T) {
	// Today is March 31 — last month (February) has only 28/29 days.
	now := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	from, to := lastMonthSamePeriod(now)
	if from.Month() != time.February || from.Day() != 1 {
		t.Errorf("from=%v", from)
	}
	// `to` should be clamped to the last day of February, not 31st.
	if to.Month() != time.February || to.Day() != 28 {
		t.Errorf("to=%v want Feb 28", to)
	}
}
