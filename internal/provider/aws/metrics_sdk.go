package aws

import (
	"context"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/tesserix/cloudnav/internal/provider"
)

// fetchCloudWatchMetricsSDK runs the same GetMetricData query the
// CLI fallback runs, but typed end-to-end via the v2 SDK. Returns
// (nil, false, err) on SDK auth failure so the caller falls back
// to `aws cloudwatch get-metric-data`.
func (a *AWS) fetchCloudWatchMetricsSDK(ctx context.Context, region string, spec *dimensionSpec, catalog []metricStat) ([]provider.Metric, bool, error) {
	cfg, err := sdkConfig(ctx)
	if err != nil {
		return nil, false, err
	}
	client := cloudwatch.NewFromConfig(cfg, func(o *cloudwatch.Options) {
		o.Region = region
	})
	end := time.Now().UTC()
	start := end.Add(-metricsWindow)
	queries := make([]cwtypes.MetricDataQuery, 0, len(catalog))
	for _, m := range catalog {
		m := m
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(m.Id),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String(spec.Namespace),
					MetricName: aws.String(m.MetricName),
					Dimensions: []cwtypes.Dimension{{
						Name:  aws.String(spec.DimensionName),
						Value: aws.String(spec.DimensionVal),
					}},
				},
				Period: aws.Int32(int32(metricsPeriodSeconds)),
				Stat:   aws.String("Average"),
			},
			ReturnData: aws.Bool(true),
		})
	}

	out, err := client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
		MetricDataQueries: queries,
		StartTime:         aws.Time(start),
		EndTime:           aws.Time(end),
	})
	if err != nil {
		return nil, true, err
	}

	// Match the CLI parser shape: one Metric per catalog entry,
	// points oldest-first.
	idToLabel := map[string]string{}
	for _, m := range catalog {
		idToLabel[m.Id] = m.Label
	}
	metrics := make([]provider.Metric, 0, len(out.MetricDataResults))
	for _, r := range out.MetricDataResults {
		id := aws.ToString(r.Id)
		label := idToLabel[id]
		if label == "" {
			label = id
		}
		// CloudWatch returns timestamps descending by default; the
		// TUI sparkline expects oldest-first.
		points := make([]float64, len(r.Values))
		stamps := make([]time.Time, len(r.Timestamps))
		copy(stamps, r.Timestamps)
		copy(points, r.Values)
		// Sort by timestamp asc to be safe across SDK ordering changes.
		idx := make([]int, len(stamps))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool {
			return stamps[idx[i]].Before(stamps[idx[j]])
		})
		ordered := make([]float64, len(points))
		for i, k := range idx {
			ordered[i] = points[k]
		}
		metrics = append(metrics, provider.Metric{
			Name:   label,
			Points: ordered,
		})
	}
	return metrics, true, nil
}
