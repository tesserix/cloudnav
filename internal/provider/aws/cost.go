package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

type costSample struct {
	amount   float64
	currency string
}

func (a *AWS) Costs(ctx context.Context, parent provider.Node) (map[string]string, error) {
	if parent.Kind != provider.KindAccount {
		return nil, fmt.Errorf("aws: cost breakdown is supported on account scope, got %q", parent.Kind)
	}
	now := time.Now().UTC()
	current, err := a.fetchCost(ctx, firstOfMonth(now), now.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("aws ce: %w", err)
	}
	fromLast, toLast := lastMonthSamePeriod(now)
	last, lastErr := a.fetchCost(ctx, fromLast, toLast.AddDate(0, 0, 1))

	out := make(map[string]string, len(current))
	for svc, cur := range current {
		if lastErr != nil || last == nil {
			out[svc] = formatCost(cur.amount, cur.currency)
			continue
		}
		lc, ok := last[svc]
		if !ok {
			out[svc] = formatCost(cur.amount, cur.currency) + " new"
			continue
		}
		out[svc] = formatCostWithDelta(cur, lc)
	}
	return out, nil
}

func (a *AWS) fetchCost(ctx context.Context, from, to time.Time) (map[string]costSample, error) {
	out, err := a.aws.Run(ctx,
		"ce", "get-cost-and-usage",
		"--time-period", fmt.Sprintf("Start=%s,End=%s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		"--granularity", "MONTHLY",
		"--metrics", "UnblendedCost",
		"--group-by", "Type=DIMENSION,Key=REGION",
		"--output", "json",
	)
	if err != nil {
		return nil, err
	}
	return parseCostUsage(out)
}

func parseCostUsage(data []byte) (map[string]costSample, error) {
	var env struct {
		ResultsByTime []struct {
			Groups []struct {
				Keys    []string `json:"Keys"`
				Metrics map[string]struct {
					Amount string `json:"Amount"`
					Unit   string `json:"Unit"`
				} `json:"Metrics"`
			} `json:"Groups"`
		} `json:"ResultsByTime"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse aws ce response: %w", err)
	}
	out := make(map[string]costSample)
	for _, r := range env.ResultsByTime {
		for _, g := range r.Groups {
			if len(g.Keys) == 0 {
				continue
			}
			m, ok := g.Metrics["UnblendedCost"]
			if !ok {
				continue
			}
			amount, err := parseAmount(m.Amount)
			if err != nil {
				continue
			}
			out[strings.ToLower(g.Keys[0])] = costSample{amount: amount, currency: m.Unit}
		}
	}
	return out, nil
}

func parseAmount(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	return v, err
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func lastMonthSamePeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	firstThis := firstOfMonth(now)
	from := firstThis.AddDate(0, -1, 0)
	to := from.AddDate(0, 0, now.Day()-1)
	lastDay := firstThis.AddDate(0, 0, -1)
	if to.After(lastDay) {
		to = lastDay
	}
	if to.Before(from) {
		to = from
	}
	return from, to
}

func formatCost(amount float64, currency string) string {
	return fmt.Sprintf("%s%.2f", currencySymbol(currency), amount)
}

func formatCostWithDelta(current, last costSample) string {
	base := formatCost(current.amount, current.currency)
	if last.amount == 0 {
		if current.amount == 0 {
			return base
		}
		return base + " new"
	}
	delta := (current.amount - last.amount) / last.amount * 100
	switch {
	case delta > 2:
		return fmt.Sprintf("%s ↑%d%%", base, int(math.Round(delta)))
	case delta < -2:
		return fmt.Sprintf("%s ↓%d%%", base, int(math.Round(-delta)))
	default:
		return base + " →"
	}
}

func currencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "USD":
		return "$"
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "INR":
		return "₹"
	case "JPY":
		return "¥"
	case "AUD":
		return "A$"
	case "CAD":
		return "C$"
	default:
		return code + " "
	}
}
