package aws

import (
	"testing"

	"github.com/tesserix/cloudnav/internal/provider"
)

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

func TestCatalogForAWSResource(t *testing.T) {
	// Every supported service maps to a non-nil catalog; anything else
	// returns nils so the caller can degrade to an empty metric list.
	cases := []struct {
		arn    string
		ns     string
		dim    string
		mapped bool
	}{
		{"arn:aws:ec2:us-east-1:123:instance/i-abc", "AWS/EC2", "InstanceId", true},
		{"arn:aws:lambda:us-east-1:123:function:my-fn", "AWS/Lambda", "FunctionName", true},
		{"arn:aws:rds:us-east-1:123:db:prod-primary", "AWS/RDS", "DBInstanceIdentifier", true},
		{"arn:aws:s3:::my-bucket", "", "", false}, // daily granularity; not mapped
		{"arn:aws:iam::123:role/my-role", "", "", false},
	}
	for _, c := range cases {
		res := provider.Node{ID: c.arn, Kind: provider.KindResource, Meta: map[string]string{"region": "us-east-1"}}
		spec, cat := catalogForAWSResource(res)
		if !c.mapped {
			if spec != nil {
				t.Errorf("%s should not be mapped, got %+v", c.arn, spec)
			}
			continue
		}
		if spec == nil {
			t.Fatalf("%s should map to a spec", c.arn)
		}
		if spec.Namespace != c.ns {
			t.Errorf("%s namespace = %q, want %q", c.arn, spec.Namespace, c.ns)
		}
		if spec.DimensionName != c.dim {
			t.Errorf("%s dimension = %q, want %q", c.arn, spec.DimensionName, c.dim)
		}
		if len(cat) == 0 {
			t.Errorf("%s catalog is empty", c.arn)
		}
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
