package gcp

import "testing"

func TestParseBQCost(t *testing.T) {
	data := []byte(`[
      {"project_id":"acme-prod","total":1234.56,"currency":"USD"},
      {"project_id":"acme-dev","total":45.21,"currency":"USD"},
      {"project_id":"","total":999,"currency":"USD"}
    ]`)
	costs, err := parseBQCost(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(costs) != 2 {
		t.Fatalf("len=%d want 2", len(costs))
	}
	if got := costs["acme-prod"]; got != "$1234.56" {
		t.Errorf("acme-prod=%q want $1234.56", got)
	}
	if got := costs["acme-dev"]; got != "$45.21" {
		t.Errorf("acme-dev=%q want $45.21", got)
	}
}

func TestFormatCostGCP(t *testing.T) {
	cases := map[string]struct {
		amount   float64
		currency string
		want     string
	}{
		"usd":     {1000, "USD", "$1000.00"},
		"gbp":     {12.5, "GBP", "£12.50"},
		"unknown": {5, "NOK", "NOK 5.00"},
		"empty":   {1, "", "$1.00"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := formatCostGCP(tc.amount, tc.currency); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
