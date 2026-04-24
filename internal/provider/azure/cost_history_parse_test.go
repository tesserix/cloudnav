package azure

import "testing"

// parseDailyCost used to compare column names with exact case. When
// Azure drifted the casing (e.g. 'costUSD' instead of 'Cost') the
// parser returned a silent empty result and the chart rendered as all
// zeroes. Tolerate case drift and surface an error when we genuinely
// can't find the columns.

func TestParseDailyCostCaseInsensitive(t *testing.T) {
	body := []byte(`{
		"properties": {
			"columns": [
				{"name": "PRETAXCOST"},
				{"name": "UsageDate"},
				{"name": "CURRENCY"}
			],
			"rows": [
				[12.34, 20260115, "GBP"],
				[5.67, 20260116, "GBP"]
			]
		}
	}`)
	m, cur, err := parseDailyCost(body)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cur != "GBP" {
		t.Errorf("currency = %q, want GBP", cur)
	}
	if len(m) != 2 {
		t.Errorf("row count = %d, want 2", len(m))
	}
	if m["2026-01-15"] != 12.34 || m["2026-01-16"] != 5.67 {
		t.Errorf("rows not parsed: %v", m)
	}
}

func TestParseDailyCostAlternateColumnNames(t *testing.T) {
	// Some Cost Management responses emit costUSD instead of PreTaxCost.
	body := []byte(`{
		"properties": {
			"columns": [
				{"name": "costUSD"},
				{"name": "BillingMonth"}
			],
			"rows": [
				[42.0, "2026-02-01"]
			]
		}
	}`)
	m, _, err := parseDailyCost(body)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if m["2026-02-01"] != 42.0 {
		t.Errorf("costUSD column not recognised, got %v", m)
	}
}

func TestParseDailyCostMissingColumnsReturnsError(t *testing.T) {
	body := []byte(`{
		"properties": {
			"columns": [{"name": "SomeUnknownField"}],
			"rows": [[1]]
		}
	}`)
	_, _, err := parseDailyCost(body)
	if err == nil {
		t.Error("expected error when cost/date columns missing — silent nil used to make the chart render as all zeroes")
	}
}
