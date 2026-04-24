package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) loadBilling() tea.Cmd {
	b, ok := m.active.(provider.Billing)
	if !ok {
		m.status = m.active.Name() + ": billing overview not supported"
		return nil
	}
	summarer, _ := m.active.(provider.BillingSummarer)
	m.loading = true
	m.status = "loading " + m.active.Name() + " billing..."
	ctx := m.ctx
	scope := m.active.Name()
	if gp, ok := m.active.(*gcp.GCP); ok {
		prov := gp
		return tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				lines, err := b.Billing(ctx)
				if err != nil {
					return errMsg{err}
				}
				status, _ := prov.BillingStatus(ctx)
				summary := scopeSummaryFrom(ctx, summarer)
				return billingLoadedMsg{lines: lines, scope: scope, gcpStatus: status, summary: summary}
			},
		)
	}
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			lines, err := b.Billing(ctx)
			if err != nil {
				return errMsg{err}
			}
			summary := scopeSummaryFrom(ctx, summarer)
			return billingLoadedMsg{lines: lines, scope: scope, summary: summary}
		},
	)
}

// scopeSummaryFrom calls the optional BillingSummary capability. Quiet on
// error because every field is strictly additive — a failed summary just
// means the TOTAL line reports per-row aggregates without a forecast or
// budget indicator, same as before BillingSummarer existed.
func scopeSummaryFrom(ctx context.Context, s provider.BillingSummarer) provider.BillingScope {
	if s == nil {
		return provider.BillingScope{}
	}
	out, err := s.BillingSummary(ctx)
	if err != nil {
		return provider.BillingScope{}
	}
	return out
}
func (m *model) updateBilling(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q", "B":
		m.billingMode = false
		m.status = ""
		return m, nil
	case keyUp, "k":
		if m.billingIdx > 0 {
			m.billingIdx--
		}
		return m, nil
	case keyDown, "j":
		if m.billingIdx < len(m.billingLines)-1 {
			m.billingIdx++
		}
		return m, nil
	case "o":
		// In GCP setup mode, o opens the Cloud Billing export console.
		if m.billingGCP != nil && m.billingGCP.SetupURL != "" {
			go openURL(m.billingGCP.SetupURL)
			m.status = "opened " + m.billingGCP.SetupURL
		}
		return m, nil
	}
	return m, nil
}

// billingView renders the cloud's portfolio-style cost breakdown: services
// (AWS), subscriptions (Azure), projects (GCP). Delta arrows reuse the same
// logic as the inline cost column so behaviour matches.
func (m *model) billingView() string {
	total := 0.0
	totalLast := 0.0
	totalForecast := 0.0
	currency := ""
	for _, l := range m.billingLines {
		total += l.Current
		totalLast += l.LastMonth
		totalForecast += l.Forecast
		if currency == "" {
			currency = l.Currency
		}
	}
	// Account-wide summary (AWS / GCP) supplements the per-row roll-up:
	// if the per-line forecast is zero but the provider reported a scope
	// forecast, surface that instead. Same for budget, which then drives
	// the TOTAL line's 🟢/🟡/🔴 indicator for providers whose budgets are
	// account-wide rather than per-line.
	if totalForecast == 0 && m.billingSummary.Forecast > 0 {
		totalForecast = m.billingSummary.Forecast
	}
	scopeBudget := m.billingSummary.Budget
	if currency == "" {
		currency = m.billingSummary.Currency
	}

	totalArrow := billingDelta(total, totalLast)
	symbol := cliCurrencySymbol(currency)
	totalIndicator := budgetIndicator(total, scopeBudget)
	totalCell := fmt.Sprintf("%s → %s%s%s", symbol+fmt.Sprintf("%.2f", totalLast), symbol, fmt.Sprintf("%.2f", total), totalArrow)
	if totalForecast > 0 {
		// Forecast is optional — only surface it in the summary when at
		// least one row produced a projection so the column doesn't read
		// "proj $0.00" on clouds that don't support forecasting yet.
		totalCell += "   proj " + symbol + fmt.Sprintf("%.2f", totalForecast)
	}
	if scopeBudget > 0 {
		pct := int(total/scopeBudget*100 + 0.5)
		totalCell += fmt.Sprintf("   %s budget %s%.0f (%d%%)", strings.TrimSpace(totalIndicator), symbol, scopeBudget, pct)
	}

	header := styles.Title.Render("billing — "+m.billingScope) + "  " +
		styles.Help.Render(fmt.Sprintf("%d line(s)   TOTAL %s", len(m.billingLines), totalCell))

	// For GCP, if the BQ export isn't live yet we have a rich diagnostic we
	// can show instead of an empty pane — walks the user through the exact
	// remaining steps to enable the export.
	if m.billingScope == providerGCP && !m.gcpExportLive() && m.billingGCP != nil {
		return m.gcpBillingSetupView(header)
	}

	box := fullScreenBox(m.width, m.height)
	if len(m.billingLines) == 0 {
		return box.Render(strings.Join([]string{
			header,
			"",
			styles.Help.Render("No billing data yet."),
			"",
			styles.Help.Render("esc close   B toggle"),
		}, "\n"))
	}

	window := 14
	if m.height > 12 {
		window = m.height - 12
	}
	if window < 5 {
		window = 5
	}
	start := 0
	if m.billingIdx >= window {
		start = m.billingIdx - window + 1
	}
	end := start + window
	if end > len(m.billingLines) {
		end = len(m.billingLines)
	}

	lines := []string{header, ""}
	if start > 0 {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}
	for i := start; i < end; i++ {
		l := m.billingLines[i]
		cur := cliCurrencySymbol(l.Currency) + fmt.Sprintf("%.2f", l.Current)
		last := cliCurrencySymbol(l.Currency) + fmt.Sprintf("%.2f", l.LastMonth)
		proj := forecastCell(l.Forecast, l.Currency)
		arrow := billingDelta(l.Current, l.LastMonth)
		indicator := budgetIndicator(l.Current, l.Budget)
		note := ""
		switch {
		case l.Note != "":
			note = "   " + styles.Help.Render(l.Note)
		case l.Budget > 0:
			pct := int(l.Current/l.Budget*100 + 0.5)
			note = "   " + styles.Help.Render(fmt.Sprintf("budget %s%.0f (%d%%)", cliCurrencySymbol(l.Currency), l.Budget, pct))
		}
		// The selection prefix expects a stable column count so rows align
		// whether or not the sub has a budget configured. "indicator" is
		// either an emoji or a padding space — both render at roughly one
		// cell's width in monospace terminals.
		row := fmt.Sprintf("%s %2d. %-40s   last %-12s   now %-12s   proj %-12s  %s%s",
			indicator, i+1, shorten(l.Label, 40), last, cur, proj, arrow, note)
		if i == m.billingIdx {
			row = styles.Selected.Render("> " + row)
		} else {
			row = "  " + row
		}
		lines = append(lines, row)
	}
	if end < len(m.billingLines) {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↓ %d more below", len(m.billingLines)-end)))
	}
	lines = append(lines, "", styles.Help.Render("  ↑↓/jk move   esc/B close"))
	return box.Render(strings.Join(lines, "\n"))
}

// gcpExportLive reports whether the BQ billing-export table is already live
// for the caller. When false the billing overlay renders a setup checklist
// instead of an empty "0 line(s)" pane.
func (m *model) gcpExportLive() bool {
	return m.billingGCP != nil && m.billingGCP.ExportTable != ""
}

// gcpBillingSetupView renders the step-by-step diagnostic for GCP BQ billing
// export: billing account, IAM roles, dataset, export table. Each line is a
// ✓ or ✗ so the user can tell at a glance which step is pending.
func (m *model) gcpBillingSetupView(header string) string {
	st := m.billingGCP
	check := func(ok bool) string {
		if ok {
			return styles.Good.Render("✓")
		}
		return styles.Bad.Render("✗")
	}
	lines := []string{header, "", styles.Header.Render("GCP billing-export setup")}
	lines = append(lines, fmt.Sprintf("  %s project             %s", check(st.Project != ""), st.Project))
	lines = append(lines, fmt.Sprintf("  %s billing account     %s", check(st.BillingAccount != ""), nonempty(st.BillingAccount, "unknown")))
	lines = append(lines, fmt.Sprintf("  %s billing enabled     %v", check(st.BillingEnabled), st.BillingEnabled))
	if len(st.Roles) > 0 {
		lines = append(lines, fmt.Sprintf("  %s your roles          %v", check(true), st.Roles))
	} else {
		lines = append(lines, fmt.Sprintf("  %s your roles          (unknown — need billing.viewer to read IAM)", check(false)))
	}
	lines = append(lines, fmt.Sprintf("  %s can admin billing   %v (need roles/billing.admin to enable export)", check(st.CanAdminBilling), st.CanAdminBilling))
	lines = append(lines, fmt.Sprintf("  %s dataset %q   exists=%v", check(st.DatasetExists), st.Dataset, st.DatasetExists))
	lines = append(lines, fmt.Sprintf("  %s export table        %s", check(st.ExportTable != ""), nonemptyOrDash(st.ExportTable)))
	lines = append(lines, "")
	lines = append(lines, styles.Header.Render("next steps"))
	if !st.DatasetExists && st.CanAdminBilling {
		lines = append(lines, "  1. run `cloudnav billing init` to create the billing_export dataset")
	}
	lines = append(lines, fmt.Sprintf("  %d. press o to open the setup page:", stepNum(st)))
	lines = append(lines, "       "+styles.Help.Render(st.SetupURL))
	lines = append(lines, "     choose project "+st.Project+", dataset "+st.Dataset+", detailed usage cost export")
	lines = append(lines, fmt.Sprintf("  %d. wait a few hours for first export data to land, then press B again", stepNum(st)+1))
	lines = append(lines, "")
	lines = append(lines, styles.Help.Render("  o open console   esc/B close"))
	return fullScreenBox(m.width, m.height).Render(strings.Join(lines, "\n"))
}

func stepNum(st *gcp.BillingStatus) int {
	if st.DatasetExists {
		return 1
	}
	return 2
}

func nonempty(a, fallback string) string {
	if a == "" {
		return fallback
	}
	return a
}

func nonemptyOrDash(s string) string {
	if s == "" {
		return emDash
	}
	return s
}

// anomalyThresholdPct is the MoM swing (in percent) at which the delta
// arrow earns a ⚠ prefix so the user's eye snaps to it. Tuned conservatively
// so typical week-over-week growth doesn't trip it.
const anomalyThresholdPct = 25.0

// billingDelta mirrors the inline-column arrow formatting so users see the
// same up/down/flat indicators across the cost column and the billing view.
// Deltas whose magnitude exceeds anomalyThresholdPct get a ⚠ prefix to flag
// potential cost spikes or unexpected drop-offs.
func billingDelta(current, last float64) string {
	if last == 0 {
		if current == 0 {
			return emDash
		}
		return styles.Good.Render("new")
	}
	d := (current - last) / last * 100
	anomaly := d >= anomalyThresholdPct || d <= -anomalyThresholdPct
	switch {
	case d > 2:
		body := fmt.Sprintf("↑%d%%", int(d+0.5))
		if anomaly {
			return styles.Bad.Render("⚠ " + body)
		}
		return styles.Bad.Render(body)
	case d < -2:
		body := fmt.Sprintf("↓%d%%", int(-d+0.5))
		if anomaly {
			return styles.Good.Render("⚠ " + body)
		}
		return styles.Good.Render(body)
	default:
		return styles.Help.Render("→")
	}
}

// budgetIndicator returns a single-rune traffic-light showing how close MTD
// spend is to the configured monthly budget. Empty when no budget is set
// so the column doesn't look noisy for un-budgeted subs.
func budgetIndicator(current, budget float64) string {
	if budget <= 0 {
		return " "
	}
	ratio := current / budget
	switch {
	case ratio >= 1.0:
		return styles.Bad.Render("🔴")
	case ratio >= 0.75:
		return styles.WarnS.Render("🟡")
	default:
		return styles.Good.Render("🟢")
	}
}

// forecastCell formats the projected month-end total cell for the billing
// overlay. Zero means "no forecast available" (first-of-month, caller lacks
// Cost Management access, or the provider doesn't compute forecasts) and
// renders as an em-dash so the column stays aligned.
func forecastCell(forecast float64, currency string) string {
	if forecast <= 0 {
		return emDash
	}
	return cliCurrencySymbol(currency) + fmt.Sprintf("%.2f", forecast)
}

// cliCurrencySymbol is a copy of the per-provider currencySymbol so the
// billing view doesn't need to reach into the azure package.
func cliCurrencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case currencyUSD, "":
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
