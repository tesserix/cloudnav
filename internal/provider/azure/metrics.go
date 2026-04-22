package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Metrics returns a short-window time-series for the given resource via
// Azure Monitor. One ARM call covers every supported metric for the
// resource — the service returns "unit unsupported" entries inline that
// we filter out so callers never see a fake 0-ish series.
//
// Scope: last 60 minutes at 5-minute granularity. Callers that need a
// longer window can re-fetch; we keep the default tight so the overlay
// stays snappy and the sparklines read cleanly at 12 data points.
func (a *Azure) Metrics(ctx context.Context, resource provider.Node) ([]provider.Metric, error) {
	if resource.Kind != provider.KindResource {
		return nil, fmt.Errorf("azure metrics: unsupported kind %q (expected resource)", resource.Kind)
	}
	subID := resource.Meta["subscriptionId"]
	if subID == "" {
		return nil, fmt.Errorf("azure metrics: resource is missing subscriptionId")
	}

	end := time.Now().UTC()
	start := end.Add(-1 * time.Hour)
	names := metricNamesForType(resource.Meta["type"])
	if len(names) == 0 {
		return []provider.Metric{}, nil
	}
	params := url.Values{}
	params.Set("api-version", "2023-10-01")
	params.Set("timespan", start.Format("2006-01-02T15:04:05Z")+"/"+end.Format("2006-01-02T15:04:05Z"))
	params.Set("interval", "PT5M")
	params.Set("aggregation", "Average")
	params.Set("metricnames", strings.Join(names, ","))
	armURL := "https://management.azure.com" + resource.ID + "/providers/microsoft.insights/metrics?" + params.Encode()
	body, err := a.getJSONForSub(ctx, subID, armURL)
	if err != nil {
		return nil, fmt.Errorf("azure monitor: %w", err)
	}
	return parseMonitorMetrics(body)
}

// metricNamesForType returns the sensible default metrics for a given
// ARM resource type. Azure Monitor complains with 400 when you ask for a
// metric that doesn't exist on the target, so we whitelist per type
// rather than guessing. Types we don't recognise get an empty list and
// the overlay renders a clear "no default metrics for <type>" message.
func metricNamesForType(armType string) []string {
	switch strings.ToLower(armType) {
	case "microsoft.compute/virtualmachines":
		return []string{"Percentage CPU", "Available Memory Bytes", "Network In Total", "Network Out Total", "Disk Read Bytes"}
	case "microsoft.web/sites":
		return []string{"CpuTime", "MemoryWorkingSet", "Requests", "Http5xx"}
	case "microsoft.sql/servers/databases":
		return []string{"cpu_percent", "dtu_used", "connection_successful", "connection_failed"}
	case "microsoft.storage/storageaccounts":
		return []string{"Transactions", "Ingress", "Egress", "Availability"}
	case "microsoft.containerservice/managedclusters":
		return []string{"node_cpu_usage_percentage", "node_memory_working_set_percentage"}
	default:
		return nil
	}
}

func parseMonitorMetrics(data []byte) ([]provider.Metric, error) {
	var env struct {
		Value []struct {
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
			Unit       string `json:"unit"`
			Timeseries []struct {
				Data []struct {
					TimeStamp string  `json:"timeStamp"`
					Average   float64 `json:"average"`
					Total     float64 `json:"total"`
					Count     float64 `json:"count"`
				} `json:"data"`
			} `json:"timeseries"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse monitor metrics: %w", err)
	}
	out := make([]provider.Metric, 0, len(env.Value))
	for _, m := range env.Value {
		if len(m.Timeseries) == 0 {
			continue
		}
		ts := m.Timeseries[0]
		if len(ts.Data) == 0 {
			continue
		}
		points := make([]float64, 0, len(ts.Data))
		for _, d := range ts.Data {
			// "Average" is empty when the aggregation didn't include a
			// value for that bin; fall through to Total / Count so we
			// don't drop the row entirely for sparse-but-real data.
			v := d.Average
			if v == 0 && d.Total != 0 {
				v = d.Total
			}
			points = append(points, v)
		}
		out = append(out, provider.Metric{
			Name:   m.Name.Value,
			Unit:   m.Unit,
			Points: points,
		})
	}
	return out, nil
}

// Ensure Azure implements Metricser at compile time.
var _ provider.Metricser = (*Azure)(nil)
