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
	points, err := parseTimeSeries(data, false)
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
	points, err := parseTimeSeries(data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0] != 1234567 {
		t.Errorf("got %v, want [1234567]", points)
	}
}

func TestParseTimeSeriesEmpty(t *testing.T) {
	points, err := parseTimeSeries([]byte(`[]`), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Errorf("got %d points, want 0", len(points))
	}
}

func TestParseTimeSeriesFirstSeries(t *testing.T) {
	// When a metric filter matches multiple series (e.g. two disks on
	// the same instance) and aggregate=false, we only consume the first.
	data := []byte(`[
		{"points":[{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":1}}]},
		{"points":[{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":999}}]}
	]`)
	points, err := parseTimeSeries(data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0] != 1 {
		t.Errorf("got %v, want [1] (first series only)", points)
	}
}

func TestParseTimeSeriesAggregateSumsSeries(t *testing.T) {
	// Disk metrics come back as one series per attached disk. With
	// aggregate=true the parser sums them bin-wise so a two-disk VM's
	// total I/O shows as the sum, not just one disk's value.
	data := []byte(`[
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:05:00Z"},"value":{"doubleValue":30}},
			{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":10}}
		]},
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:05:00Z"},"value":{"doubleValue":20}},
			{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":5}}
		]}
	]`)
	points, err := parseTimeSeries(data, true)
	if err != nil {
		t.Fatal(err)
	}
	// After reverse: series1=[10,30], series2=[5,20]; summed=[15,50].
	if len(points) != 2 || points[0] != 15 || points[1] != 50 {
		t.Errorf("got %v, want [15 50]", points)
	}
}

func TestParseTimeSeriesAggregateTruncatesShortest(t *testing.T) {
	// If a disk was attached mid-window, one series has fewer bins.
	// The aggregate truncates to the shorter so the sparkline stays
	// well-formed.
	data := []byte(`[
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:10:00Z"},"value":{"doubleValue":3}},
			{"interval":{"endTime":"2026-04-22T00:05:00Z"},"value":{"doubleValue":2}},
			{"interval":{"endTime":"2026-04-22T00:00:00Z"},"value":{"doubleValue":1}}
		]},
		{"points":[
			{"interval":{"endTime":"2026-04-22T00:10:00Z"},"value":{"doubleValue":30}}
		]}
	]`)
	points, err := parseTimeSeries(data, true)
	if err != nil {
		t.Fatal(err)
	}
	// After reverse: s1=[1,2,3], s2=[30]; truncated to len=1; summed=[31].
	if len(points) != 1 || points[0] != 31 {
		t.Errorf("got %v, want [31]", points)
	}
}

func TestIsDiskMetric(t *testing.T) {
	if !isDiskMetric("compute.googleapis.com/instance/disk/read_bytes_count") {
		t.Error("disk read should be flagged")
	}
	if isDiskMetric("compute.googleapis.com/instance/cpu/utilization") {
		t.Error("cpu should not be flagged")
	}
}
