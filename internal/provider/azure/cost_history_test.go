package azure

import (
	"testing"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseDailyCostIntegerDate(t *testing.T) {
	// Cost Management returns UsageDate as a yyyymmdd integer when
	// granularity=Daily. Make sure we normalise that correctly.
	data := []byte(`{
      "properties": {
        "columns": [
          {"name": "PreTaxCost", "type": "Number"},
          {"name": "UsageDate", "type": "Number"},
          {"name": "Currency", "type": "String"}
        ],
        "rows": [
          [12.5, 20260115, "GBP"],
          [7.25, 20260116, "GBP"],
          [3.0,  20260115, "GBP"]
        ]
      }
    }`)
	out, cur, err := parseDailyCost(data)
	if err != nil {
		t.Fatal(err)
	}
	if cur != "GBP" {
		t.Fatalf("currency=%q want GBP", cur)
	}
	if got := out["2026-01-15"]; got != 15.5 {
		t.Errorf("2026-01-15=%v want 15.5 (sum of two rows)", got)
	}
	if got := out["2026-01-16"]; got != 7.25 {
		t.Errorf("2026-01-16=%v want 7.25", got)
	}
}

func TestParseDailyCostStringDate(t *testing.T) {
	data := []byte(`{
      "properties": {
        "columns": [
          {"name": "PreTaxCost"},
          {"name": "Date"},
          {"name": "Currency"}
        ],
        "rows": [
          [1.0, "2026-02-01T00:00:00Z", "USD"]
        ]
      }
    }`)
	out, cur, err := parseDailyCost(data)
	if err != nil {
		t.Fatal(err)
	}
	if cur != "USD" {
		t.Errorf("currency=%q want USD", cur)
	}
	if out["2026-02-01"] != 1.0 {
		t.Errorf("2026-02-01=%v want 1.0", out["2026-02-01"])
	}
}

func TestBucketByMonth(t *testing.T) {
	pts := []provider.CostHistoryPoint{
		{Date: mustDate("2026-01-15"), Amount: 10},
		{Date: mustDate("2026-01-20"), Amount: 5},
		{Date: mustDate("2026-02-01"), Amount: 30},
		{Date: mustDate("2026-02-15"), Amount: 7},
	}
	out := bucketByMonth(pts)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].Amount != 15 || out[1].Amount != 37 {
		t.Errorf("totals=%v,%v want 15,37", out[0].Amount, out[1].Amount)
	}
	// Each bucket should be anchored to the 1st of the month so the
	// downstream chart can sort predictably.
	if out[0].Date.Day() != 1 || out[1].Date.Day() != 1 {
		t.Errorf("dates not anchored to 1st: %v %v", out[0].Date, out[1].Date)
	}
}

func TestBucketByWeekAnchorsMonday(t *testing.T) {
	// Thursday 2026-01-15 belongs to the week starting Monday 2026-01-12.
	pts := []provider.CostHistoryPoint{
		{Date: mustDate("2026-01-15"), Amount: 4},
		{Date: mustDate("2026-01-16"), Amount: 2},
	}
	out := bucketByWeek(pts)
	if len(out) != 1 {
		t.Fatalf("len=%d want 1 (same week)", len(out))
	}
	if out[0].Amount != 6 {
		t.Errorf("total=%v want 6", out[0].Amount)
	}
	if out[0].Date.Weekday() != time.Monday {
		t.Errorf("bucket anchor is %s want Monday", out[0].Date.Weekday())
	}
}

func TestAggregateMonths(t *testing.T) {
	pts := []provider.CostHistoryPoint{
		{Date: mustDate("2026-01-15"), Amount: 10},
		{Date: mustDate("2026-01-20"), Amount: 5},
		{Date: mustDate("2026-02-01"), Amount: 30},
		{Date: mustDate("2026-02-15"), Amount: 7},
	}
	out := aggregateMonths(pts)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2", len(out))
	}
	if out[0].Total != 15 || out[1].Total != 37 {
		t.Errorf("totals=%v,%v want 15,37", out[0].Total, out[1].Total)
	}
	if out[0].Month.String() != "January" {
		t.Errorf("first month=%s want January", out[0].Month)
	}
}
