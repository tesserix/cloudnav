package azure

import "testing"

func TestMetricNamesForTypeKnown(t *testing.T) {
	// Type match is case-insensitive (Azure returns mixed case depending
	// on which API you ask) so the whitelist has to handle that.
	if names := metricNamesForType("Microsoft.Compute/virtualMachines"); len(names) == 0 {
		t.Error("VM should have default metrics")
	}
	if names := metricNamesForType("microsoft.web/sites"); len(names) == 0 {
		t.Error("App Service should have default metrics")
	}
}

func TestMetricNamesForTypeUnknown(t *testing.T) {
	// Unknown types intentionally return an empty list so the overlay
	// can show a clean "no metrics for <type>" message instead of a 400
	// from Azure Monitor complaining the metric doesn't exist.
	if names := metricNamesForType("Microsoft.Something/unknown"); len(names) != 0 {
		t.Errorf("unknown type returned %v, want none", names)
	}
}

func TestParseMonitorMetrics(t *testing.T) {
	data := []byte(`{
		"value":[
			{"name":{"value":"Percentage CPU"},"unit":"Percent","timeseries":[{"data":[{"timeStamp":"2026-04-22T00:00:00Z","average":12.5},{"timeStamp":"2026-04-22T00:05:00Z","average":18.2}]}]},
			{"name":{"value":"Network In Total"},"unit":"Bytes","timeseries":[{"data":[{"timeStamp":"2026-04-22T00:00:00Z","average":0,"total":5432},{"timeStamp":"2026-04-22T00:05:00Z","average":0,"total":6100}]}]},
			{"name":{"value":"Empty"},"unit":"Count","timeseries":[{"data":[]}]}
		]
	}`)
	metrics, err := parseMonitorMetrics(data)
	if err != nil {
		t.Fatal(err)
	}
	// Empty series drops out — otherwise the overlay renders a flat zero
	// line that's indistinguishable from "workload is idle".
	if len(metrics) != 2 {
		t.Fatalf("got %d series, want 2 (empty should drop)", len(metrics))
	}
	if metrics[0].Name != "Percentage CPU" {
		t.Errorf("name = %q", metrics[0].Name)
	}
	if metrics[0].Unit != "Percent" {
		t.Errorf("unit = %q", metrics[0].Unit)
	}
	if len(metrics[0].Points) != 2 || metrics[0].Points[0] != 12.5 {
		t.Errorf("points = %v", metrics[0].Points)
	}
	// Network falls through from Average to Total when Average is zero.
	if metrics[1].Points[0] != 5432 {
		t.Errorf("total-fallback = %v, want 5432", metrics[1].Points[0])
	}
}
