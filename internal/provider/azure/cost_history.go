package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// CostHistory returns a cost time-series rolled up across every
// subscription the caller has access to, respecting the window and
// bucket requested in opts. Defaults (zero-value opts) are 90 days
// daily so the `$` shortcut opens with a sensible 3-month chart.
//
// Days is clamped to [1, 400] so a typo can't fan out to a multi-year
// scan; Bucket falls back to BucketDay when empty.
func (a *Azure) CostHistory(ctx context.Context, opts provider.CostHistoryOptions) (provider.CostHistory, error) {
	days := opts.Days
	if days <= 0 {
		days = 90
	}
	if days > 400 {
		days = 400
	}
	bucket := opts.Bucket
	if bucket == "" {
		bucket = provider.BucketDay
	}

	var subs []subJSON
	if opts.Scope != "" {
		// Single-subscription scope — skip the SDK call, trust what the
		// caller handed us. Keeps the "I know which sub" path cheap.
		subs = []subJSON{{ID: opts.Scope, Name: opts.ScopeLabel}}
	} else {
		ids, err := a.subIDs(ctx)
		if err != nil {
			return provider.CostHistory{}, fmt.Errorf("azure cost history: %w", err)
		}
		subs = make([]subJSON, 0, len(ids))
		for _, id := range ids {
			subs = append(subs, subJSON{ID: id, Name: a.subName(id)})
		}
	}

	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	var from time.Time
	if bucket == provider.BucketMonth {
		// Monthly bucket: start at the first of the month that's `days`
		// ago so every rendered point covers a full calendar month.
		start := now.AddDate(0, 0, -days+1)
		from = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	}

	if len(subs) == 0 {
		return provider.CostHistory{
			Scope:      "azure",
			Currency:   defaultCurrency,
			Series:     provider.CostSeries{Label: "all subscriptions", Currency: defaultCurrency},
			Bucket:     bucket,
			WindowDays: days,
		}, nil
	}

	type subResult struct {
		points   map[string]float64 // yyyy-mm-dd → amount
		currency string
		err      error
	}

	results := make(chan subResult, len(subs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, s := range subs {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pts, cur, err := a.querySubDailyCost(ctx, s.ID, from, to)
			results <- subResult{points: pts, currency: cur, err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	merged := map[string]float64{}
	currency := ""
	failed := 0
	succeeded := 0
	authDenied := 0
	for r := range results {
		if r.err != nil {
			failed++
			if isAuthDenied(r.err) {
				authDenied++
			}
			continue
		}
		succeeded++
		if currency == "" {
			currency = r.currency
		}
		for day, amt := range r.points {
			merged[day] += amt
		}
	}
	if currency == "" {
		currency = defaultCurrency
	}

	// Build a contiguous daily series across [from, to]. Month-bucketed
	// output is derived from this same daily base so totals stay
	// consistent with the summary strip.
	dailyPoints := make([]provider.CostHistoryPoint, 0)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		dailyPoints = append(dailyPoints, provider.CostHistoryPoint{Date: d, Amount: merged[key]})
	}

	var seriesPoints []provider.CostHistoryPoint
	switch bucket {
	case provider.BucketMonth:
		seriesPoints = bucketByMonth(dailyPoints)
	case provider.BucketWeek:
		seriesPoints = bucketByWeek(dailyPoints)
	default:
		seriesPoints = dailyPoints
	}

	scopeLabel := "azure · all subscriptions"
	seriesLabel := fmt.Sprintf("azure · %d subscription(s)", succeeded)
	if opts.Scope != "" {
		name := opts.ScopeLabel
		if name == "" {
			name = opts.Scope
		}
		scopeLabel = "azure · " + name
		seriesLabel = name
	}

	history := provider.CostHistory{
		Scope:    scopeLabel,
		Currency: currency,
		Series: provider.CostSeries{
			Label:    seriesLabel,
			Currency: currency,
			Points:   seriesPoints,
		},
		Months:     aggregateMonths(dailyPoints),
		Bucket:     bucket,
		WindowDays: days,
	}
	// AccessDenied drives the "press P to jump to PIM" footer. For a
	// single-scope query we know definitively; for a fan-out it's only
	// meaningful when EVERY sub failed with an auth error (otherwise the
	// chart has real data and the footer would confuse more than help).
	if opts.Scope != "" && authDenied > 0 && succeeded == 0 {
		history.AccessDenied = true
	}
	if opts.Scope == "" && succeeded == 0 && authDenied > 0 {
		history.AccessDenied = true
	}
	if failed > 0 {
		history.Note = fmt.Sprintf("%d subscription(s) omitted (no cost-read access)", failed)
	}
	return history, nil
}

// querySubDailyCost runs a single Cost Management query with daily
// granularity over the requested window. Returns a map keyed by
// YYYY-MM-DD so the caller can merge across subs without worrying about
// timezone offsets.
func (a *Azure) querySubDailyCost(ctx context.Context, subID string, from, to time.Time) (map[string]float64, string, error) {
	body := map[string]any{
		"type":      "ActualCost",
		"timeframe": timeframeCustom,
		"timePeriod": map[string]any{
			"from": from.UTC().Format("2006-01-02T15:04:05Z"),
			"to":   to.UTC().Format("2006-01-02T15:04:05Z"),
		},
		"dataset": map[string]any{
			"granularity": "Daily",
			"aggregation": map[string]any{
				"totalCost": map[string]any{"name": "PreTaxCost", "function": "Sum"},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.CostManagement/query?api-version=2023-11-01",
		subID,
	)
	out, err := a.postJSONForSub(ctx, subID, url, raw)
	if err != nil {
		return nil, "", err
	}
	return parseDailyCost(out)
}

// parseDailyCost unpacks the Cost Management envelope into a
// YYYY-MM-DD→amount map and the response currency. Cost Management
// returns date as a yyyymmdd integer when granularity=Daily so we
// normalise it here.
func parseDailyCost(data []byte) (map[string]float64, string, error) {
	var env struct {
		Properties struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, "", fmt.Errorf("parse daily cost: %w", err)
	}
	// Cost Management returns column names in the casing Azure
	// chooses — usually the names we requested, but be tolerant about
	// casing drift. Match lowercased so a future API tweak doesn't
	// silently nuke the entire chart.
	costCol, dateCol, currencyCol := -1, -1, -1
	for i, c := range env.Properties.Columns {
		switch strings.ToLower(c.Name) {
		case strings.ToLower(colPreTaxCost), strings.ToLower(colCost), "costusd", "actualcost":
			costCol = i
		case "usagedate", "billingmonth", "date":
			dateCol = i
		case strings.ToLower(colCurrency), "currencycode":
			currencyCol = i
		}
	}
	if costCol < 0 || dateCol < 0 {
		cols := make([]string, 0, len(env.Properties.Columns))
		for _, c := range env.Properties.Columns {
			cols = append(cols, c.Name)
		}
		return nil, "", fmt.Errorf("cost response missing cost/date column (got %s)", strings.Join(cols, ", "))
	}
	out := make(map[string]float64, len(env.Properties.Rows))
	currency := defaultCurrency
	for _, r := range env.Properties.Rows {
		if len(r) <= costCol || len(r) <= dateCol {
			continue
		}
		amount, ok := r[costCol].(float64)
		if !ok {
			continue
		}
		var key string
		switch v := r[dateCol].(type) {
		case float64:
			// Comes in as yyyymmdd integer.
			n := int(v)
			key = fmt.Sprintf("%04d-%02d-%02d", n/10000, (n/100)%100, n%100)
		case string:
			if len(v) >= 10 {
				key = v[:10]
			} else {
				key = v
			}
		default:
			continue
		}
		out[key] += amount
		if currencyCol >= 0 && len(r) > currencyCol {
			if c, ok := r[currencyCol].(string); ok && c != "" {
				currency = c
			}
		}
	}
	return out, currency, nil
}

// aggregateMonths buckets a daily series into per-month totals in
// chronological order, so the TUI can label month boundaries and compute
// month-over-month percentage deltas.
func aggregateMonths(points []provider.CostHistoryPoint) []provider.CostMonth {
	totals := map[string]*provider.CostMonth{}
	order := []string{}
	for _, p := range points {
		key := fmt.Sprintf("%04d-%02d", p.Date.Year(), int(p.Date.Month()))
		m, ok := totals[key]
		if !ok {
			m = &provider.CostMonth{Year: p.Date.Year(), Month: p.Date.Month()}
			totals[key] = m
			order = append(order, key)
		}
		m.Total += p.Amount
	}
	sort.Strings(order)
	out := make([]provider.CostMonth, 0, len(order))
	for _, k := range order {
		out = append(out, *totals[k])
	}
	return out
}

// bucketByMonth collapses a daily series into one point per calendar
// month. Each output point is dated to the first of the month so the
// renderer can sort and place month ticks without additional metadata.
func bucketByMonth(points []provider.CostHistoryPoint) []provider.CostHistoryPoint {
	totals := map[string]*provider.CostHistoryPoint{}
	order := []string{}
	for _, p := range points {
		key := fmt.Sprintf("%04d-%02d", p.Date.Year(), int(p.Date.Month()))
		pt, ok := totals[key]
		if !ok {
			pt = &provider.CostHistoryPoint{
				Date: time.Date(p.Date.Year(), p.Date.Month(), 1, 0, 0, 0, 0, time.UTC),
			}
			totals[key] = pt
			order = append(order, key)
		}
		pt.Amount += p.Amount
	}
	sort.Strings(order)
	out := make([]provider.CostHistoryPoint, 0, len(order))
	for _, k := range order {
		out = append(out, *totals[k])
	}
	return out
}

// bucketByWeek collapses a daily series into ISO-week buckets. The
// week's Monday is used as the bucket date so adjacent weeks sort
// correctly across year boundaries.
func bucketByWeek(points []provider.CostHistoryPoint) []provider.CostHistoryPoint {
	totals := map[string]*provider.CostHistoryPoint{}
	order := []string{}
	for _, p := range points {
		monday := p.Date
		// Go's Weekday: Sunday=0 ... Saturday=6. We want Monday as the
		// bucket anchor, so shift the offset appropriately.
		offset := int(monday.Weekday()) - 1
		if offset < 0 {
			offset = 6
		}
		monday = monday.AddDate(0, 0, -offset)
		key := monday.Format("2006-01-02")
		pt, ok := totals[key]
		if !ok {
			pt = &provider.CostHistoryPoint{Date: monday}
			totals[key] = pt
			order = append(order, key)
		}
		pt.Amount += p.Amount
	}
	sort.Strings(order)
	out := make([]provider.CostHistoryPoint, 0, len(order))
	for _, k := range order {
		out = append(out, *totals[k])
	}
	return out
}

// isAuthDenied matches the usual spellings Azure uses for permission
// failures so the overlay can offer a PIM elevation hand-off instead of
// a generic "nothing here" message.
func isAuthDenied(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, kw := range []string{"AuthorizationFailed", "Forbidden", "does not have authorization", "403"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// Ensure Azure implements CostHistoryer at compile time.
var _ provider.CostHistoryer = (*Azure)(nil)
