package aws

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/tesserix/cloudnav/internal/provider"
)

// CostHistory satisfies provider.CostHistoryer for AWS. Powers the
// `$` overlay at parity with Azure / GCP. Uses Cost Explorer
// GetCostAndUsage with daily / monthly granularity.
//
// Note: AWS Cost Explorer doesn't support a WEEKLY granularity
// natively. When the caller asks for week buckets we fetch daily
// data and roll up to weekly buckets in process — same shape the
// chart layer expects.
func (a *AWS) CostHistory(ctx context.Context, opts provider.CostHistoryOptions) (provider.CostHistory, error) {
	days := opts.Days
	if days <= 0 {
		days = 90
	}
	bucket := opts.Bucket
	if bucket == "" {
		bucket = provider.BucketDay
	}

	scope := opts.Scope
	scopeLabel := opts.ScopeLabel
	if scopeLabel == "" {
		if scope == "" {
			scopeLabel = "all accounts"
		} else {
			scopeLabel = scope
		}
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)

	// Cost Explorer's End is exclusive, so add a day so the chart
	// includes today.
	endQuery := end.AddDate(0, 0, 1)

	client, err := a.ceClient(ctx)
	if err != nil || client == nil {
		return provider.CostHistory{
			Bucket:     bucket,
			WindowDays: days,
			Note:       "AWS Cost Explorer SDK unavailable — sign in with `aws sso login` and retry",
		}, nil
	}

	gran := cetypes.GranularityDaily
	if bucket == provider.BucketMonth {
		gran = cetypes.GranularityMonthly
	}

	in := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format("2006-01-02")),
			End:   aws.String(endQuery.Format("2006-01-02")),
		},
		Granularity: gran,
		Metrics:     []string{"UnblendedCost"},
	}
	if scope != "" {
		in.Filter = &cetypes.Expression{
			Dimensions: &cetypes.DimensionValues{
				Key:    cetypes.DimensionLinkedAccount,
				Values: []string{scope},
			},
		}
	}

	out, err := client.GetCostAndUsage(ctx, in)
	if err != nil {
		return provider.CostHistory{
			Bucket:     bucket,
			WindowDays: days,
			Note:       fmt.Sprintf("Cost Explorer query failed: %s", firstLineAWS(err.Error())),
		}, nil
	}

	currency := defaultCurrency
	points := make([]provider.CostHistoryPoint, 0, len(out.ResultsByTime))
	for _, p := range out.ResultsByTime {
		if p.TimePeriod == nil {
			continue
		}
		date, perr := time.Parse("2006-01-02", aws.ToString(p.TimePeriod.Start))
		if perr != nil {
			continue
		}
		m, ok := p.Total["UnblendedCost"]
		if !ok {
			continue
		}
		amount, perr := strconv.ParseFloat(aws.ToString(m.Amount), 64)
		if perr != nil {
			continue
		}
		if u := aws.ToString(m.Unit); u != "" {
			currency = u
		}
		points = append(points, provider.CostHistoryPoint{Date: date, Amount: amount})
	}

	if bucket == provider.BucketWeek {
		points = rollUpToWeekly(points)
	}

	sort.SliceStable(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })

	months := bucketMonthsAWS(points)
	return provider.CostHistory{
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
	}, nil
}

// rollUpToWeekly takes daily points and groups them into ISO weeks
// (Monday-anchored) summing each bucket. Used because Cost Explorer
// doesn't support a WEEKLY granularity directly.
func rollUpToWeekly(points []provider.CostHistoryPoint) []provider.CostHistoryPoint {
	buckets := map[string]*provider.CostHistoryPoint{}
	for _, p := range points {
		// Monday of this point's week.
		offset := int(p.Date.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		monday := p.Date.AddDate(0, 0, -offset)
		key := monday.Format("2006-01-02")
		b, ok := buckets[key]
		if !ok {
			b = &provider.CostHistoryPoint{Date: monday}
			buckets[key] = b
		}
		b.Amount += p.Amount
	}
	out := make([]provider.CostHistoryPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	return out
}

// bucketMonthsAWS rolls daily / weekly points into calendar-month
// totals for the overlay's compare strip.
func bucketMonthsAWS(points []provider.CostHistoryPoint) []provider.CostMonth {
	totals := map[string]*provider.CostMonth{}
	for _, p := range points {
		key := fmt.Sprintf("%04d-%02d", p.Date.Year(), p.Date.Month())
		m, ok := totals[key]
		if !ok {
			m = &provider.CostMonth{Year: p.Date.Year(), Month: p.Date.Month()}
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

func firstLineAWS(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// Compile-time assert AWS implements CostHistoryer at parity with
// Azure and GCP.
var _ provider.CostHistoryer = (*AWS)(nil)
