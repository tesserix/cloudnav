// Package currency converts numeric cost amounts between currencies
// using the public frankfurter.app feed (ECB-backed, free, no API
// key). Rates are cached in SQLite via cache.Shared so the same
// conversions across cloudnav launches don't re-hit the network.
//
// Failure mode: every consumer is expected to gracefully fall back
// to the native currency on FX failures. Cost rendering must never
// block on the network.
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// frankfurterURL is the public Frankfurter API endpoint. Kept as a
// var rather than a const so tests can substitute an httptest server.
var frankfurterURL = "https://api.frankfurter.app/latest"

// LatestRates fetches the day's rates from `base` to every other
// supported currency. Returns a map keyed by upper-case currency
// code (e.g. "EUR", "GBP", "USD") mapping to the multiplier used as
// `target = amount * rates[target]`.
//
// The API returns rates relative to the requested base, so a
// {"USD": 1.05, "EUR": 1.0} response with base=EUR means
// "1 EUR is 1.05 USD" — multiply EUR amounts by 1.05 to convert.
//
// The 5-second timeout keeps the cost overlay snappy even when
// frankfurter is rate-limiting.
func LatestRates(ctx context.Context, base string) (map[string]float64, error) {
	base = strings.ToUpper(strings.TrimSpace(base))
	if base == "" {
		base = "USD"
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s?from=%s", frankfurterURL, base)
	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cloudnav/fx")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("frankfurter %d: %s", resp.StatusCode, trimBody(body))
	}
	var payload struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("frankfurter decode: %w", err)
	}
	if payload.Rates == nil {
		return nil, fmt.Errorf("frankfurter returned no rates for base %q", base)
	}
	// Frankfurter omits the base currency from the rates map (a
	// rate of 1.0 to itself). We add it explicitly so the
	// converter handles same-currency conversions without an
	// extra special case.
	out := make(map[string]float64, len(payload.Rates)+1)
	for k, v := range payload.Rates {
		out[strings.ToUpper(k)] = v
	}
	out[strings.ToUpper(payload.Base)] = 1.0
	return out, nil
}

func trimBody(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
