package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// gcpMetricsWindow mirrors the Azure / AWS defaults — 60 minutes at
// 5-minute bins — so sparklines across clouds line up visually.
const gcpMetricsWindow = 1 * time.Hour

// gcpMetricCatalog maps human-readable labels to Cloud Monitoring metric
// types + units for the handful of compute metrics we fetch by default.
// Extending to other asset types is a one-line addition per row.
type gcpMetric struct {
	Label      string
	MetricType string
	Unit       string
	Rate       bool // true when we should rate-convert (bytes → bytes/s)
}

var gcpInstanceMetrics = []gcpMetric{
	{Label: "CPU %", MetricType: "compute.googleapis.com/instance/cpu/utilization", Unit: "%"},
	{Label: "Net In B/s", MetricType: "compute.googleapis.com/instance/network/received_bytes_count", Unit: "Bytes", Rate: true},
	{Label: "Net Out B/s", MetricType: "compute.googleapis.com/instance/network/sent_bytes_count", Unit: "Bytes", Rate: true},
	{Label: "Disk Read B/s", MetricType: "compute.googleapis.com/instance/disk/read_bytes_count", Unit: "Bytes", Rate: true},
	{Label: "Disk Write B/s", MetricType: "compute.googleapis.com/instance/disk/write_bytes_count", Unit: "Bytes", Rate: true},
}

// Metrics returns short-window time-series for a GCP resource. Only
// compute.googleapis.com/Instance is wired today; other asset types
// return an empty slice so the overlay renders the existing
// "no default metrics for this type" message.
//
// Cloud Monitoring lives at the project level — we take the project id
// from Meta and ask for time series filtered to this specific instance
// via its resource.labels.instance_id label.
func (g *GCP) Metrics(ctx context.Context, resource provider.Node) ([]provider.Metric, error) {
	if resource.Kind != provider.KindResource {
		return nil, fmt.Errorf("gcp metrics: unsupported kind %q (expected resource)", resource.Kind)
	}
	if !strings.EqualFold(resource.Meta["type"], "compute.googleapis.com/Instance") {
		return nil, nil
	}
	project := resource.Meta["project"]
	if project == "" {
		return nil, fmt.Errorf("gcp metrics: resource is missing project metadata")
	}
	// Cloud Monitoring's instance metrics key on instance_id or
	// instance_name. Use the display name (shortName of the asset id)
	// which the TUI already stores as Node.Name.
	instanceName := resource.Name
	if instanceName == "" {
		return nil, nil
	}

	out := make([]provider.Metric, 0, len(gcpInstanceMetrics))
	for _, m := range gcpInstanceMetrics {
		points, err := g.fetchTimeSeries(ctx, project, instanceName, m.MetricType, m.Rate)
		if err != nil || len(points) == 0 {
			continue
		}
		out = append(out, provider.Metric{
			Name:   m.Label,
			Unit:   m.Unit,
			Points: points,
		})
	}
	return out, nil
}

// fetchTimeSeries runs one `gcloud monitoring time-series list` call
// scoped to the project and the specific instance. Reducer choice is
// important — bytes counters need RATE so the sparkline shows bytes per
// second instead of an ever-growing integer.
func (g *GCP) fetchTimeSeries(ctx context.Context, project, instance, metricType string, rate bool) ([]float64, error) {
	end := time.Now().UTC()
	start := end.Add(-gcpMetricsWindow)
	filter := fmt.Sprintf("metric.type=%q AND metric.labels.instance_name=%q", metricType, instance)

	args := []string{
		"monitoring", "time-series", "list",
		"--project=" + project,
		"--filter=" + filter,
		"--start-time=" + start.Format("2006-01-02T15:04:05Z"),
		"--end-time=" + end.Format("2006-01-02T15:04:05Z"),
		"--format=json",
	}
	if rate {
		// 5-minute aligned rate matches the Azure / AWS PT5M default.
		args = append(args,
			"--aggregation-alignment-period=300s",
			"--aggregation-per-series-aligner=ALIGN_RATE",
		)
	} else {
		args = append(args,
			"--aggregation-alignment-period=300s",
			"--aggregation-per-series-aligner=ALIGN_MEAN",
		)
	}

	out, err := g.gcloud.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseTimeSeries(out)
}

// parseTimeSeries pivots `gcloud monitoring time-series list` output into
// a single []float64. Cloud Monitoring returns most-recent-first — we
// reverse so the sparkline reads left-to-right like the AWS and Azure
// overlays. Values live under either doubleValue or int64Value depending
// on the metric; we coerce both into float64.
func parseTimeSeries(data []byte) ([]float64, error) {
	var series []struct {
		Points []struct {
			Interval struct {
				EndTime string `json:"endTime"`
			} `json:"interval"`
			Value struct {
				DoubleValue *float64 `json:"doubleValue"`
				Int64Value  *string  `json:"int64Value"`
			} `json:"value"`
		} `json:"points"`
	}
	if err := json.Unmarshal(data, &series); err != nil {
		return nil, fmt.Errorf("parse gcp monitoring: %w", err)
	}
	if len(series) == 0 {
		return nil, nil
	}
	// Take the first series — each metric.type + label filter typically
	// matches one series per instance. If Cloud Monitoring returns
	// multiple (e.g. one per disk), we show the first and skip the
	// rest; callers wanting disk-by-disk detail can drill in the
	// console for now.
	pts := make([]float64, 0, len(series[0].Points))
	for _, p := range series[0].Points {
		switch {
		case p.Value.DoubleValue != nil:
			pts = append(pts, *p.Value.DoubleValue)
		case p.Value.Int64Value != nil:
			var v float64
			_, _ = fmt.Sscanf(*p.Value.Int64Value, "%f", &v)
			pts = append(pts, v)
		}
	}
	// Reverse in place so index-0 is the oldest point.
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
	return pts, nil
}

// Ensure GCP satisfies the Metricser interface at compile time.
var _ provider.Metricser = (*GCP)(nil)
