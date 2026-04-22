package gcp

import "testing"

func TestParseTimeSeriesReverses(t *testing.T) {
	// Cloud Monitoring returns newest-first. parseTimeSeries reverses
	// so the sparkline reads oldest→newest, matching AWS and Azure.
	data := []byte(`[
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:25:00Z"},"value":{"doubleValue":0.55}},
			{"interval":{"endTime":"2026-04-22T00:20:00Z"},"value":{"doubleValue":0.40}},
			{"interval":{"endTime":"2026-04-22T00:15:00Z"},"value":{"doubleValue":0.25}}
		]}
	]`)
	points, err := parseTimeSeries(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.25, 0.40, 0.55}
	for i, v := range points {
		if v != want[i] {
			t.Errorf("points[%d] = %v, want %v", i, v, want[i])
		}
	}
}

func TestParseTimeSeriesInt64Coercion(t *testing.T) {
	// Cloud Monitoring returns counter-style metrics as int64Value
	// strings — parseTimeSeries coerces them to float64 so the unified
	// Metric.Points type works.
	data := []byte(`[
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:10:00Z"},"value":{"int64Value":"1234567"}}
		]}
	]`)
	points, err := parseTimeSeries(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0] != 1234567 {
		t.Errorf("got %v, want [1234567]", points)
	}
}

func TestParseTimeSeriesEmpty(t *testing.T) {
	points, err := parseTimeSeries([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Errorf("got %d points, want 0", len(points))
	}
}

func TestParseTimeSeriesFirstSeries(t *testing.T) {
	// When a metric filter matches multiple series (e.g. two disks on
	// the same instance), we only consume the first. Documented in the
	// code — users wanting per-disk detail drill in the console.
	data := []byte(`[
		{"points":[{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":1}}]},
		{"points":[{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":999}}]}
	]`)
	points, err := parseTimeSeries(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0] != 1 {
		t.Errorf("got %v, want [1] (first series only)", points)
	}
}
