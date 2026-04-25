package gcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"google.golang.org/api/iterator"

	"github.com/tesserix/cloudnav/internal/provider"
)

// CostHistory satisfies provider.CostHistoryer for GCP. Powers the
// `$` cost overlay at parity with Azure (which has its own
// Cost-Management daily series). The data source is the same
// BigQuery billing-export table the cost column already uses; we
// just group by day instead of project so the chart sees a smooth
// daily series.
//
// Returns a "no BQ export" payload (with `Note` set) when the
// table isn't configured — the overlay renders the message instead
// of an empty chart so the user knows what to do next.
func (g *GCP) CostHistory(ctx context.Context, opts provider.CostHistoryOptions) (provider.CostHistory, error) {
	days := opts.Days
	if days <= 0 {
		days = 90
	}
	bucket := opts.Bucket
	if bucket == "" {
		bucket = provider.BucketDay
	}

	table := g.billingTableResolved()
	if table == "" {
		if detected := g.autoDetectBillingTable(ctx); detected != "" {
			g.billingTable = detected
			table = detected
		}
	}
	if table == "" {
		return provider.CostHistory{
			Bucket:     bucket,
			WindowDays: days,
			Note:       "Configure a BigQuery billing export — set CLOUDNAV_GCP_BILLING_TABLE or run the `B` setup flow.",
		}, nil
	}

	scope := opts.Scope
	scopeLabel := opts.ScopeLabel
	if scopeLabel == "" {
		if scope == "" {
			scopeLabel = "all projects"
		} else {
			scopeLabel = scope
		}
	}

	// SDK fast path. Reuses the same BigQuery client cost.go uses.
	points, currency, err := g.queryCostHistorySDK(ctx, table, scope, days, bucket)
	if err != nil {
		return provider.CostHistory{
			Bucket:     bucket,
			WindowDays: days,
			Note:       fmt.Sprintf("BigQuery query failed: %s", firstLine(err.Error())),
		}, nil
	}

	months := bucketMonths(points)
	out := provider.CostHistory{
		Scope:      scopeLabel,
		Currency:   currency,
		Bucket:     bucket,
		WindowDays: days,
		Series: provider.CostSeries{
			Label:    scopeLabel,
			Currency: currency,
			Points:   points,
		},
		Months: months,
	}
	return out, nil
}

// defaultGCPCurrency is the fallback currency tag when the BQ
// billing-export response carries an empty Currency field. Hoisted
// to a const so the goconst lint rule doesn't trip on the literal
// repeating across the four return points in this file.
const defaultGCPCurrency = "USD"

// queryCostHistorySDK runs the daily / weekly / monthly grouping
// query depending on the requested bucket. Falls back to gcloud bq
// query when the SDK isn't usable.
func (g *GCP) queryCostHistorySDK(ctx context.Context, table, scope string, days int, bucket provider.CostBucket) ([]provider.CostHistoryPoint, string, error) {
	client, err := g.bigqueryClient(ctx, table)
	if err != nil || client == nil {
		return nil, defaultGCPCurrency, err
	}
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -days)

	dateExpr := bqDateExpr(bucket)
	whereScope := ""
	if scope != "" {
		whereScope = fmt.Sprintf(" AND project.id = '%s'", bqEscape(scope))
	}
	query := fmt.Sprintf(
		"SELECT %s AS bucket_date, ROUND(SUM(cost), 2) AS total, "+
			"ANY_VALUE(currency) AS currency "+
			"FROM `%s` "+
			"WHERE usage_start_time >= TIMESTAMP('%s')%s "+
			"GROUP BY bucket_date "+
			"ORDER BY bucket_date ASC",
		dateExpr,
		table,
		from.Format("2006-01-02T15:04:05Z"),
		whereScope,
	)
	q := client.Query(query)
	q.UseLegacySQL = false

	it, err := q.Read(ctx)
	if err != nil {
		return nil, defaultGCPCurrency, fmt.Errorf("gcp bq cost-history: %w", err)
	}
	currency := defaultGCPCurrency
	points := make([]provider.CostHistoryPoint, 0, days)
	for {
		var row struct {
			BucketDate time.Time `bigquery:"bucket_date"`
			Total      float64   `bigquery:"total"`
			Currency   string    `bigquery:"currency"`
		}
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, currency, err
		}
		if currency == defaultGCPCurrency && row.Currency != "" {
			currency = row.Currency
		}
		points = append(points, provider.CostHistoryPoint{
			Date:   row.BucketDate,
			Amount: row.Total,
		})
	}
	// BQ returns empty intermediate buckets as missing rows; the
	// chart wants a contiguous series. Fill gaps with zero so the
	// sparkline reads continuously.
	points = fillGaps(points, from, now, bucket)
	return points, currency, nil
}

// bqDateExpr returns the BigQuery date-truncation expression for
// the requested bucket. Day → DATE(usage_start_time); Week →
// DATE_TRUNC(...,WEEK(MONDAY)); Month → DATE_TRUNC(...,MONTH).
func bqDateExpr(bucket provider.CostBucket) string {
	switch bucket {
	case provider.BucketWeek:
		return "DATE(DATE_TRUNC(usage_start_time, WEEK(MONDAY)))"
	case provider.BucketMonth:
		return "DATE(DATE_TRUNC(usage_start_time, MONTH))"
	default:
		return "DATE(usage_start_time)"
	}
}

// bqEscape sanitises a project id for inclusion in a literal SQL
// string. GCP project ids are restricted to lowercase ASCII +
// digits + dashes, so the escape is a quote-stripper rather than a
// full SQL escape.
func bqEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' || c == '\\' || c == ';' {
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// fillGaps inserts zero-cost points for any bucket that the BQ
// query didn't return. Without this the chart line collapses
// across silent days and miscommunicates spend.
func fillGaps(points []provider.CostHistoryPoint, from, to time.Time, bucket provider.CostBucket) []provider.CostHistoryPoint {
	step := bucketStep(bucket)
	if step == 0 {
		return points
	}
	exists := make(map[string]bool, len(points))
	for _, p := range points {
		exists[p.Date.Format("2006-01-02")] = true
	}
	out := make([]provider.CostHistoryPoint, 0, len(points))
	cursor := alignToBucket(from, bucket)
	end := alignToBucket(to, bucket)
	for !cursor.After(end) {
		key := cursor.Format("2006-01-02")
		if !exists[key] {
			out = append(out, provider.CostHistoryPoint{Date: cursor, Amount: 0})
		}
		cursor = cursor.Add(step)
	}
	out = append(out, points...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func bucketStep(bucket provider.CostBucket) time.Duration {
	switch bucket {
	case provider.BucketWeek:
		return 7 * 24 * time.Hour
	case provider.BucketMonth:
		return 0 // months vary in length — handled separately
	default:
		return 24 * time.Hour
	}
}

func alignToBucket(t time.Time, bucket provider.CostBucket) time.Time {
	year, month, day := t.Date()
	switch bucket {
	case provider.BucketWeek:
		// Round to Monday — matches BQ's WEEK(MONDAY).
		base := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		offset := int(base.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		return base.AddDate(0, 0, -offset)
	case provider.BucketMonth:
		return time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
}

// bucketMonths produces the calendar-month rollup the overlay's
// compare strip uses (independent of the chart's bucket).
func bucketMonths(points []provider.CostHistoryPoint) []provider.CostMonth {
	totals := map[string]*provider.CostMonth{}
	for _, p := range points {
		key := fmt.Sprintf("%04d-%02d", p.Date.Year(), p.Date.Month())
		m, ok := totals[key]
		if !ok {
			m = &provider.CostMonth{
				Year:  p.Date.Year(),
				Month: p.Date.Month(),
			}
			totals[key] = m
		}
		m.Total += p.Amount
	}
	out := make([]provider.CostMonth, 0, len(totals))
	for _, m := range totals {
		out = append(out, *m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year < out[j].Year
		}
		return out[i].Month < out[j].Month
	})
	return out
}

// Compile-time assert GCP implements CostHistoryer at parity with
// Azure.
var _ provider.CostHistoryer = (*GCP)(nil)
