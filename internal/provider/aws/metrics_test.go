package aws

import "testing"

func TestParseMetricDataReverses(t *testing.T) {
	// CloudWatch returns timestamps newest-first. The parser reverses
	// the Values slice so sparklines read left-to-right oldest→newest.
	data := []byte(`{
		"MetricDataResults":[
			{"Id":"cpu","Label":"CPUUtilization","Timestamps":["2026-04-22T00:25:00Z","2026-04-22T00:20:00Z","2026-04-22T00:15:00Z"],"Values":[40,30,10]}
		]
	}`)
	catalog := []metricStat{{Id: "cpu", MetricName: "CPUUtilization", Label: "CPU %"}}
	metrics, err := parseMetricData(data, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("len = %d, want 1", len(metrics))
	}
	// After reversal: oldest first → [10, 30, 40].
	want := []float64{10, 30, 40}
	for i, v := range metrics[0].Points {
		if v != want[i] {
			t.Errorf("points[%d] = %v, want %v (reversed)", i, v, want[i])
		}
	}
	if metrics[0].Name != "CPU %" {
		t.Errorf("name = %q — catalog label should win over API label", metrics[0].Name)
	}
}

func TestParseMetricDataSkipsEmpty(t *testing.T) {
	// A metric that returned no values for the window shouldn't render
	// as a flat-zero line — better to drop it than mislead the reader.
	data := []byte(`{
		"MetricDataResults":[
			{"Id":"cpu","Label":"CPU","Timestamps":[],"Values":[]},
			{"Id":"netin","Label":"In","Timestamps":["2026-04-22T00:00:00Z"],"Values":[5]}
		]
	}`)
	catalog := []metricStat{
		{Id: "cpu", MetricName: "CPUUtilization", Label: "CPU %"},
		{Id: "netin", MetricName: "NetworkIn", Label: "Net In"},
	}
	metrics, err := parseMetricData(data, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("got %d, want 1 (empty should drop)", len(metrics))
	}
	if metrics[0].Name != "Net In" {
		t.Errorf("name = %q", metrics[0].Name)
	}
}

func TestUnitForMetric(t *testing.T) {
	cases := map[string]string{
		"CPUUtilization": "%",
		"NetworkIn":      "Bytes",
		"DiskReadBytes":  "Bytes",
		"UnknownMetric":  "",
	}
	for in, want := range cases {
		if got := unitForMetric(in); got != want {
			t.Errorf("unitForMetric(%q) = %q, want %q", in, got, want)
		}
	}
}
