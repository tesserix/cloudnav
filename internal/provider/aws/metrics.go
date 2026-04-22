package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// metricsWindow is the default lookback for the metrics overlay. Sixty
// minutes at 5-minute granularity gives twelve bins — matches the Azure
// implementation so sparklines across clouds align visually.
const metricsWindow = 1 * time.Hour

// metricsPeriodSeconds is the CloudWatch bin size in seconds. 300 = 5m,
// which is the finest granularity available across all metrics (some
// only publish at 5m) without tripping the "Detailed Monitoring"
// surcharge.
const metricsPeriodSeconds = 300

// Metrics returns short-window time-series for an AWS resource. Only
// EC2 is wired today; other namespaces (Lambda, RDS, S3, DynamoDB)
// return an empty slice with a Meta-embedded hint so the overlay can
// show a clear "not wired for this type" message instead of failing.
//
// CloudWatch is region-scoped — we always pass the resource's home
// region explicitly rather than relying on the default profile region.
func (a *AWS) Metrics(ctx context.Context, resource provider.Node) ([]provider.Metric, error) {
	if resource.Kind != provider.KindResource {
		return nil, fmt.Errorf("aws metrics: unsupported kind %q (expected resource)", resource.Kind)
	}
	region := resource.Meta["region"]
	if region == "" {
		return nil, fmt.Errorf("aws metrics: resource is missing region metadata")
	}

	// ARN → (service, instance-id). Only EC2 mapped for now; everything
	// else returns an empty slice so the TUI's "no metrics" branch
	// renders cleanly.
	service := serviceFromARN(resource.ID)
	restype := resourceTypeFromARN(resource.ID)
	if service != "ec2" || restype != "instance" {
		return nil, nil
	}
	instanceID := nameFromARN(resource.ID)
	if instanceID == "" {
		return nil, nil
	}

	return a.fetchEC2Metrics(ctx, region, instanceID)
}

// metricStat pairs a CloudWatch metric name with the label we want the
// overlay to show. Pulled out of fetchEC2Metrics so parseMetricData can
// accept a slice of it without resorting to anonymous-struct wizardry.
type metricStat struct {
	Id         string
	MetricName string
	Label      string
}

// fetchEC2Metrics issues a single cloudwatch:GetMetricData call covering
// every EC2 metric we care about. Batched so we make one API call per
// resource rather than five.
func (a *AWS) fetchEC2Metrics(ctx context.Context, region, instanceID string) ([]provider.Metric, error) {
	end := time.Now().UTC()
	start := end.Add(-metricsWindow)

	catalog := []metricStat{
		{Id: "cpu", MetricName: "CPUUtilization", Label: "CPU %"},
		{Id: "netin", MetricName: "NetworkIn", Label: "Net In B/s"},
		{Id: "netout", MetricName: "NetworkOut", Label: "Net Out B/s"},
		{Id: "diskr", MetricName: "DiskReadBytes", Label: "Disk Read B/s"},
		{Id: "diskw", MetricName: "DiskWriteBytes", Label: "Disk Write B/s"},
	}
	queries := make([]map[string]any, 0, len(catalog))
	for _, m := range catalog {
		queries = append(queries, map[string]any{
			"Id": m.Id,
			"MetricStat": map[string]any{
				"Metric": map[string]any{
					"Namespace":  "AWS/EC2",
					"MetricName": m.MetricName,
					"Dimensions": []map[string]string{
						{"Name": "InstanceId", "Value": instanceID},
					},
				},
				"Period": metricsPeriodSeconds,
				"Stat":   "Average",
			},
			"ReturnData": true,
		})
	}
	body, err := json.Marshal(queries)
	if err != nil {
		return nil, err
	}

	out, err := a.aws.Run(ctx,
		"cloudwatch", "get-metric-data",
		"--region", region,
		"--metric-data-queries", string(body),
		"--start-time", start.Format("2006-01-02T15:04:05Z"),
		"--end-time", end.Format("2006-01-02T15:04:05Z"),
		"--output", "json",
	)
	if err != nil {
		return nil, fmt.Errorf("cloudwatch get-metric-data: %w", err)
	}
	return parseMetricData(out, catalog)
}

// parseMetricData pivots CloudWatch's GetMetricData response into the
// normalised provider.Metric shape. CloudWatch returns timestamps in
// descending order by default — we reverse them so the sparkline reads
// left-to-right like every other chart on the planet.
func parseMetricData(data []byte, catalog []metricStat) ([]provider.Metric, error) {
	var env struct {
		MetricDataResults []struct {
			Id         string    `json:"Id"`
			Label      string    `json:"Label"`
			Timestamps []string  `json:"Timestamps"`
			Values     []float64 `json:"Values"`
		} `json:"MetricDataResults"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse cloudwatch response: %w", err)
	}
	byID := map[string]struct {
		name string
		unit string
	}{}
	for _, m := range catalog {
		byID[m.Id] = struct {
			name string
			unit string
		}{name: m.Label, unit: unitForMetric(m.MetricName)}
	}
	out := make([]provider.Metric, 0, len(env.MetricDataResults))
	for _, r := range env.MetricDataResults {
		if len(r.Values) == 0 {
			continue
		}
		// Reverse in place — CloudWatch sorts newest → oldest.
		pts := make([]float64, len(r.Values))
		for i, v := range r.Values {
			pts[len(r.Values)-1-i] = v
		}
		info := byID[r.Id]
		name := info.name
		if name == "" {
			name = r.Label
		}
		out = append(out, provider.Metric{
			Name:   name,
			Unit:   info.unit,
			Points: pts,
		})
	}
	return out, nil
}

// unitForMetric maps the AWS-native metric names to human-readable units
// that match the sparkline header. Kept small on purpose — the catalog
// only has the five EC2 metrics, so we don't need a full map of every
// namespace.
func unitForMetric(name string) string {
	switch {
	case name == "CPUUtilization":
		return "%"
	case strings.HasSuffix(name, "Bytes"), strings.HasPrefix(name, "Network"):
		return "Bytes"
	}
	return ""
}

// Ensure AWS satisfies the Metricser interface at compile time.
var _ provider.Metricser = (*AWS)(nil)
