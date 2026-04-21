package azure

import (
	"context"
	"encoding/json"
	"fmt"
)

// querySubBudget returns the *largest* monthly budget configured on the
// subscription, or zero when no budget is set. Azure lets customers define
// multiple budgets per scope (prod / non-prod / team, etc.); the TUI is
// single-row so we pick the biggest ceiling as the sub-wide cap.
//
// A missing-auth response is non-fatal and returns (0, nil) rather than an
// error — the caller simply leaves Budget=0 and the indicator stays blank,
// which is the same behaviour as "no budget configured".
func (a *Azure) querySubBudget(ctx context.Context, subID string) (float64, error) {
	url := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/providers/Microsoft.Consumption/budgets?api-version=2023-11-01",
		subID,
	)
	out, err := a.getJSONForSub(ctx, subID, url)
	if err != nil {
		// Treat forbidden / not-found as "no budget available" so the UI
		// degrades silently. Real failures (500s, network) still return
		// the error so the caller can decide what to do.
		if looksBenignBudgetErr(err) {
			return 0, nil
		}
		return 0, err
	}
	return parseLargestBudget(out)
}

func looksBenignBudgetErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, needle := range []string{"403", "404", "AuthorizationFailed", "does not have authorization", "NotFound"} {
		if containsFold(s, needle) {
			return true
		}
	}
	return false
}

// parseLargestBudget walks the budgets response and returns the max
// `amount` across all budgets scoped to this sub.
func parseLargestBudget(data []byte) (float64, error) {
	var env struct {
		Value []struct {
			Properties struct {
				Amount    float64 `json:"amount"`
				TimeGrain string  `json:"timeGrain"`
			} `json:"properties"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, fmt.Errorf("parse budgets: %w", err)
	}
	var max float64
	for _, b := range env.Value {
		// Stick to monthly budgets for the TUI column — quarterly and
		// annual ceilings don't compare cleanly against MTD actuals.
		if b.Properties.TimeGrain != "" && b.Properties.TimeGrain != "Monthly" {
			continue
		}
		if b.Properties.Amount > max {
			max = b.Properties.Amount
		}
	}
	return max, nil
}

// containsFold is a lowercase substring check used by the benign-error
// matcher so mixed-case ARM error payloads still match.
func containsFold(s, needle string) bool {
	if len(needle) == 0 || len(s) < len(needle) {
		return len(needle) == 0
	}
	n, h := []byte(needle), []byte(s)
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := 0; j < len(n); j++ {
			a, b := h[i+j], n[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
