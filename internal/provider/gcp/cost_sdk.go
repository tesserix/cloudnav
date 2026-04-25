package gcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

// BigQuery client lifecycle. Same lazy + cached-error pattern. The
// project the client is scoped to is the *billing host* project —
// the one that owns the billing-export dataset, not the project the
// cost rows describe.
var (
	bqOnce    sync.Once
	bqClient  *bigquery.Client
	bqProject string // the project the client is scoped to
	bqInitErr error
)

// bigqueryClient returns the process-shared BQ client. We resolve
// the host project from the cached billing-table path
// (`<project>.<dataset>.<table>`) so the SDK knows where to send
// the query — BQ jobs run in a billing project, not against a table
// path alone.
func (g *GCP) bigqueryClient(ctx context.Context, table string) (*bigquery.Client, error) {
	hostProject := bqHostProject(table)
	if hostProject == "" {
		return nil, fmt.Errorf("gcp bq: can't resolve host project from table %q (expected <project>.<dataset>.<table>)", table)
	}
	bqOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := bigquery.NewClient(c, hostProject)
		if err != nil {
			bqInitErr = err
			return
		}
		bqClient = client
		bqProject = hostProject
	})
	if bqInitErr == nil && bqProject != hostProject && bqClient != nil {
		// Caller supplied a different host project than the one we
		// opened with. BQ clients are pinned to a project so we open
		// a fresh one rather than reuse the cached handle.
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client, err := bigquery.NewClient(c, hostProject)
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	return bqClient, bqInitErr
}

// queryProjectCostsSDK runs the same BQ query the gcloud path uses
// (sum cost per project this month-to-date) but via the BigQuery SDK.
// Returns (nil, false, err) when the SDK isn't usable so the caller
// falls back to `gcloud bq query`.
func (g *GCP) queryProjectCostsSDK(ctx context.Context, table string) (map[string]string, bool, error) {
	if table == "" {
		return nil, false, nil
	}
	client, err := g.bigqueryClient(ctx, table)
	if err != nil || client == nil {
		return nil, false, err
	}
	q := client.Query(fmt.Sprintf(
		"SELECT project.id AS project_id, ROUND(SUM(cost), 2) AS total, currency "+
			"FROM `%s` "+
			"WHERE usage_start_time >= TIMESTAMP_TRUNC(CURRENT_TIMESTAMP(), MONTH) "+
			"GROUP BY project_id, currency",
		table,
	))
	q.UseLegacySQL = false
	it, err := q.Read(ctx)
	if err != nil {
		return nil, true, fmt.Errorf("gcp bq SDK: %w", err)
	}
	out := map[string]string{}
	for {
		var row struct {
			ProjectID string  `bigquery:"project_id"`
			Total     float64 `bigquery:"total"`
			Currency  string  `bigquery:"currency"`
		}
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, true, err
		}
		if row.ProjectID == "" {
			continue
		}
		out[strings.ToLower(row.ProjectID)] = formatCostGCP(row.Total, row.Currency)
	}
	return out, true, nil
}

// bqHostProject returns the first segment of a `project.dataset.table`
// path. BigQuery jobs need to be billed somewhere; we use the
// project that owns the export table by default. Empty string when
// the input doesn't have the expected three-segment shape.
func bqHostProject(table string) string {
	parts := strings.SplitN(table, ".", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func closeBQClient() error {
	if bqClient != nil {
		return bqClient.Close()
	}
	return nil
}
