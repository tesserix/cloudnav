package gcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Cloud Monitoring SDK client lifecycle.
var (
	monOnce    sync.Once
	monClient  *monitoring.MetricClient
	monInitErr error
)

func (g *GCP) monitoringClient(ctx context.Context) (*monitoring.MetricClient, error) {
	monOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := monitoring.NewMetricClient(c)
		if err != nil {
			monInitErr = err
			return
		}
		monClient = client
	})
	return monClient, monInitErr
}

// fetchTimeSeriesSDK queries Cloud Monitoring v3 ListTimeSeries via
// the SDK. Returns (nil, false, err) when ADC isn't usable so the
// caller falls through to gcloud.
//
// Reducer / aligner choices match the gcloud CLI path verbatim so
// the sparkline shape doesn't shift between the two paths.
func (g *GCP) fetchTimeSeriesSDK(ctx context.Context, project, instance, metricType string, rate, aggregate bool) ([]float64, bool, error) {
	client, err := g.monitoringClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	end := time.Now().UTC()
	start := end.Add(-gcpMetricsWindow)
	filter := fmt.Sprintf("metric.type=%q AND metric.labels.instance_name=%q", metricType, instance)

	aligner := monitoringpb.Aggregation_ALIGN_MEAN
	if rate {
		aligner = monitoringpb.Aggregation_ALIGN_RATE
	}

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + project,
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:  &durationpb.Duration{Seconds: 300},
			PerSeriesAligner: aligner,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	it := client.ListTimeSeries(ctx, req)

	// Each series carries its own []point; we either aggregate
	// across series (sum at each timestamp) or take the first
	// series only.
	bySeries := make([][]metricSample, 0, 4)
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		series := make([]metricSample, 0, len(ts.Points))
		for _, p := range ts.Points {
			if p == nil || p.Interval == nil || p.Value == nil {
				continue
			}
			val := pointValue(p.Value)
			series = append(series, metricSample{
				t: p.Interval.EndTime.AsTime(),
				v: val,
			})
		}
		bySeries = append(bySeries, series)
		if !aggregate {
			break
		}
	}
	if len(bySeries) == 0 {
		return nil, true, nil
	}

	// Cloud Monitoring returns most-recent-first per series. The
	// caller (TUI sparkline) wants oldest-first, so we reverse on
	// flatten.
	if !aggregate {
		return reverseFloats(extractValues(bySeries[0])), true, nil
	}
	// Aggregate: bucket by timestamp, sum each bucket.
	buckets := map[time.Time]float64{}
	for _, s := range bySeries {
		for _, p := range s {
			buckets[p.t] += p.v
		}
	}
	flat := make([]metricSample, 0, len(buckets))
	for t, v := range buckets {
		flat = append(flat, metricSample{t: t, v: v})
	}
	sortByTime(flat)
	out := make([]float64, len(flat))
	for i, p := range flat {
		out[i] = p.v
	}
	return out, true, nil
}

type metricSample struct {
	t time.Time
	v float64
}

func pointValue(v *monitoringpb.TypedValue) float64 {
	switch x := v.Value.(type) {
	case *monitoringpb.TypedValue_DoubleValue:
		return x.DoubleValue
	case *monitoringpb.TypedValue_Int64Value:
		return float64(x.Int64Value)
	case *monitoringpb.TypedValue_BoolValue:
		if x.BoolValue {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func extractValues(samples []metricSample) []float64 {
	out := make([]float64, len(samples))
	for i, s := range samples {
		out[i] = s.v
	}
	return out
}

func reverseFloats(in []float64) []float64 {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}

func sortByTime(in []metricSample) {
	// Insertion sort — N is small (12 buckets at the 60-min/5-min
	// default), no need to drag in sort.Slice and a closure.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j-1].t.After(in[j].t); j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
}

func closeMonitoringClient() error {
	if monClient != nil {
		return monClient.Close()
	}
	return nil
}
