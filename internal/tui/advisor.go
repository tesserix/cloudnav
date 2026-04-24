package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) loadAdvisor() tea.Cmd {
	adv, ok := m.active.(provider.Advisor)
	if !ok {
		m.status = m.active.Name() + ": advisor not supported"
		return nil
	}
	scopeID, filterScope, displayName := m.advisorScopeForActive()
	if scopeID == "" {
		m.status = "advisor needs a subscription / project scope — drill in first"
		return nil
	}
	// Snapshot the cursor row for the resource-context header. Only
	// meaningful when the advisor is scoped to a single resource;
	// otherwise we leave it zero-valued and the popup falls back to
	// the wider scope label.
	m.advisorResource = provider.Node{}
	if m.atResourceLevel() {
		if c := m.table.Cursor(); c >= 0 && c < len(m.visibleNodes) {
			m.advisorResource = m.visibleNodes[c]
		}
	}
	m.loading = true
	m.status = "loading " + m.active.Name() + " advisor recommendations..."
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			recs, err := adv.Recommendations(ctx, scopeID)
			if err != nil {
				return errMsg{err}
			}
			filtered := filterAdvisorByScope(recs, filterScope)
			return advisorLoadedMsg{recs: filtered, scope: filterScope, scopeName: displayName}
		},
	)
}

// advisorScopeForActive resolves the advisor scope for the current provider.
// Returns (apiScope, filterScope, display) where apiScope is what gets
// passed to the provider (e.g. the sub id / project id) and filterScope is
// the string client-side filtering uses to narrow results to the cursor.
func (m *model) advisorScopeForActive() (string, string, string) {
	switch m.active.Name() {
	case pimSrcAzure:
		subID, rgName, resourceID, name := m.advisorTarget()
		filter := "/subscriptions/" + subID
		if rgName != "" {
			filter += "/resourceGroups/" + rgName
		}
		if resourceID != "" {
			filter = resourceID
		}
		return subID, filter, name
	case providerGCP:
		projID, name := m.gcpAdvisorTarget()
		// On a resource row, filter to just that resource's own
		// Recommender output rather than every suggestion across the
		// whole project.
		if m.atResourceLevel() {
			if c := m.table.Cursor(); c >= 0 && c < len(m.visibleNodes) {
				n := m.visibleNodes[c]
				return projID, n.ID, n.Name
			}
		}
		return projID, "projects/" + projID, name
	case "aws":
		// Compute Optimizer / Trusted Advisor return recommendations
		// spanning the whole account; there is no per-scope API. We
		// still pass a non-empty scope so the load fires, and filter
		// client-side by the resource's ARN when the user pressed A on
		// a resource row.
		scope := "account"
		name := "AWS account"
		if len(m.stack) > 0 {
			if top := &m.stack[len(m.stack)-1]; top.parent != nil {
				name = top.parent.Name
			}
		}
		if m.atResourceLevel() {
			if c := m.table.Cursor(); c >= 0 && c < len(m.visibleNodes) {
				n := m.visibleNodes[c]
				return scope, n.ID, n.Name
			}
		}
		return scope, "", name
	}
	return "", "", ""
}

func (m *model) gcpAdvisorTarget() (string, string) {
	if len(m.stack) == 0 {
		return "", ""
	}
	top := &m.stack[len(m.stack)-1]
	c := m.table.Cursor()
	if kindOf(top) == provider.KindProject {
		if c >= 0 && c < len(m.visibleNodes) {
			return m.visibleNodes[c].ID, m.visibleNodes[c].Name
		}
	}
	// Already drilled into a project — use the parent.
	if top.parent != nil && top.parent.Kind == provider.KindProject {
		return top.parent.ID, top.parent.Name
	}
	return "", ""
}

func filterAdvisorByScope(recs []provider.Recommendation, scope string) []provider.Recommendation {
	scopeLow := strings.ToLower(scope)
	out := make([]provider.Recommendation, 0, len(recs))
	for _, r := range recs {
		target := strings.ToLower(r.ResourceID)
		if target == "" || strings.HasPrefix(target, scopeLow) {
			out = append(out, r)
		}
	}
	return out
}

// advisorTarget returns the (subID, rgName, resourceID, display) tuple for
// the cursor's current scope.
func (m *model) advisorTarget() (string, string, string, string) {
	if len(m.stack) == 0 {
		return "", "", "", ""
	}
	top := &m.stack[len(m.stack)-1]
	c := m.table.Cursor()
	var cursor *provider.Node
	if c >= 0 && c < len(m.visibleNodes) {
		cursor = &m.visibleNodes[c]
	}
	switch kindOf(top) {
	case provider.KindSubscription:
		if cursor != nil {
			return cursor.ID, "", "", cursor.Name
		}
	case provider.KindResourceGroup:
		if cursor != nil {
			sub := cursor.Meta["subscriptionId"]
			return sub, cursor.Name, "", cursor.Name
		}
		if top.parent != nil {
			return top.parent.ID, "", "", top.parent.Name
		}
	case provider.KindResource:
		if cursor != nil {
			sub := cursor.Meta["subscriptionId"]
			return sub, parentRGName(cursor.ID), cursor.ID, cursor.Name
		}
		if top.parent != nil {
			sub := top.parent.Meta["subscriptionId"]
			if sub == "" && top.parent.Parent != nil {
				sub = top.parent.Parent.ID
			}
			return sub, top.parent.Name, "", top.parent.Name
		}
	}
	return "", "", "", ""
}

// parentRGName pulls the RG name out of a full Azure resource ID.
func parentRGName(id string) string {
	const marker = "/resourceGroups/"
	i := strings.Index(id, marker)
	if i < 0 {
		return ""
	}
	rest := id[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}
func (m *model) updateAdvisor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When the filter input is active, keys feed it — mirrors the PIM
	// filter behaviour so the two overlays feel identical to the user.
	if m.advisorFilterOn {
		switch msg.String() {
		case keyEsc:
			m.advisorFilter = ""
			m.advisorFilterIn.SetValue("")
			m.advisorFilterIn.Blur()
			m.advisorFilterOn = false
			m.advisorIdx = 0
			return m, nil
		case keyEnter:
			m.advisorFilterIn.Blur()
			m.advisorFilterOn = false
			return m, nil
		case keyUp, keyDown, "pgup", "pgdown":
			// Swallow — otherwise the cursor moves mid-typing and the row
			// that gets acted on isn't the one the user thinks they chose.
			return m, nil
		}
		var cmd tea.Cmd
		m.advisorFilterIn, cmd = m.advisorFilterIn.Update(msg)
		m.advisorFilter = strings.ToLower(strings.TrimSpace(m.advisorFilterIn.Value()))
		m.advisorIdx = 0
		return m, cmd
	}

	filt := m.filteredAdvisor()
	switch msg.String() {
	case keyEsc, "q", "A":
		m.advisorMode = false
		m.advisorFilter = ""
		m.advisorFilterIn.SetValue("")
		m.advisorFilterOn = false
		m.status = ""
		return m, nil
	case "/":
		m.advisorFilterOn = true
		m.advisorFilterIn.Focus()
		return m, nil
	case keyUp, "k":
		if m.advisorIdx > 0 {
			m.advisorIdx--
		}
		return m, nil
	case keyDown, "j":
		if m.advisorIdx < len(filt)-1 {
			m.advisorIdx++
		}
		return m, nil
	case "o":
		if m.advisorIdx >= 0 && m.advisorIdx < len(filt) {
			go openURL("https://portal.azure.com/#blade/Microsoft_Azure_Expert/AdvisorMenuBlade/overview")
			m.status = "opened Advisor in portal"
		}
		return m, nil
	}
	return m, nil
}

// filteredAdvisor returns advisor recommendations matching the current
// filter (case-insensitive substring against category, impact, problem
// text, solution, and resource id). Empty filter returns the full slice.
func (m *model) filteredAdvisor() []provider.Recommendation {
	q := m.advisorFilter
	if q == "" {
		return m.advisorRecs
	}
	out := make([]provider.Recommendation, 0, len(m.advisorRecs))
	for _, r := range m.advisorRecs {
		if advisorMatchesFilter(r, q) {
			out = append(out, r)
		}
	}
	return out
}

// advisorMatchesFilter reports whether a single recommendation contains q
// (already lowercased) in any of the fields a user would reasonably search
// on. Kept package-local and small so it shows up in test coverage.
func advisorMatchesFilter(r provider.Recommendation, q string) bool {
	fields := [...]string{
		strings.ToLower(r.Category),
		strings.ToLower(r.Impact),
		strings.ToLower(r.Problem),
		strings.ToLower(r.Solution),
		strings.ToLower(r.ImpactedName),
		strings.ToLower(r.ImpactedType),
		strings.ToLower(r.ResourceID),
	}
	for _, f := range fields {
		if strings.Contains(f, q) {
			return true
		}
	}
	return false
}
func (m *model) advisorView() string {
	filt := m.filteredAdvisor()
	advisorName := advisorLabelFor(m.active)

	// Single-resource path gets the compact popup with resource context.
	// Broader scopes (sub / account / project) still use the full-screen
	// table because they can return dozens of rows across many targets.
	if m.advisorResource.ID != "" {
		return m.advisorResourceCard(advisorName, filt)
	}
	return m.advisorFullView(advisorName, filt)
}

// advisorResourceCard renders the per-resource popup. Layout:
//
//   Azure Advisor  ·  cost
//   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
//   cito-civica-dev-uks-aks
//   microsoft.containerservice/managedclusters · uksouth · Base · GBP 5.60/30d
//
//   ── 1/2 ──────────────────────────  ● MEDIUM ──
//   Enable Vertical Pod Autoscaler recommendation mode to
//   rightsize resource requests and limits.
//   → Enable Vertical Pod Autoscaler recommendation mode to
//     rightsize resource requests and limits.
//
//   ── 2/2 ──────────────────────────  ● MEDIUM ──
//   Consider Spot nodes for workloads that can handle
//   interruptions.
//   → Enable Spot nodes on nodepools where the workload is
//     interruption-tolerant.
//
//   ────────────────────────────────────────────
//   2 recommendations · 2 medium · 0 high
//
//   esc  close
func (m *model) advisorResourceCard(advisorName string, filt []provider.Recommendation) string {
	r := m.advisorResource
	ruleW := 46 // consistent divider width — popup content is ~50 cells
	lines := []string{}

	// Title — "<Advisor> — <category> recommendations".
	title := styles.ModalTitle.Render(advisorName)
	if sub := dominantCategory(filt); sub != "" {
		title += "  " + styles.ModalLabel.Render("— "+sub+" recommendations")
	}
	lines = append(lines, title, "")

	// Resource-context block — aligned label rows so the popup reads
	// like the Azure portal's resource header rather than a one-liner
	// that loses the type / SKU / cost-window detail.
	labelW := 11
	addMeta := func(label, value string) {
		if value == "" {
			return
		}
		lines = append(lines, "  "+styles.ModalLabel.Render(padRight(label+":", labelW))+" "+styles.ModalValue.Render(value))
	}
	addMeta("Resource", r.Name)
	addMeta("Type", r.Meta["type"])
	switch {
	case parentRGName(r.ID) != "":
		addMeta("Group", parentRGName(r.ID))
	case r.Meta["project"] != "":
		addMeta("Project", r.Meta["project"])
	case r.Meta["accountId"] != "":
		addMeta("Account", r.Meta["accountId"])
	}
	addMeta("Region", r.Location)
	addMeta("SKU", r.Meta["sku"])
	addMeta("Cost / 30d", costWindowLine(r.Cost))
	lines = append(lines, "")

	// Empty-state fallback.
	if len(filt) == 0 {
		lines = append(lines,
			styles.ModalHint.Render(advisorEmptyHint(m.active)),
			"",
			styles.ModalHint.Render("esc  close"),
		)
		return m.overlay(strings.Join(lines, "\n"))
	}

	// Scrollable card window — ↑/↓ move advisorIdx, we render only the
	// cards that fit from that offset. Prevents the popup from spilling
	// off the bottom when the scope has many recommendations.
	//
	// Budget: popup inner height minus the chrome we've already used
	// (title + rule + resource summary + blank + footer rule + summary
	// + blank + hint). Each card takes ~3 or 4 lines (divider + problem
	// + optional → solution + blank).
	popupH := m.height - 10
	if popupH < 12 {
		popupH = 12
	}
	cardH := 4
	fit := popupH / cardH
	if fit < 1 {
		fit = 1
	}
	start := m.advisorIdx
	if start < 0 {
		start = 0
	}
	if start > len(filt)-fit && len(filt)-fit > 0 {
		start = len(filt) - fit
	} else if start > len(filt)-1 {
		start = 0
	}
	end := start + fit
	if end > len(filt) {
		end = len(filt)
	}

	if start > 0 {
		lines = append(lines, styles.ModalHint.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}

	for i := start; i < end; i++ {
		rec := filt[i]
		num := fmt.Sprintf("── %d/%d ", i+1, len(filt))
		right := "  " + impactBullet(rec.Impact) + " "
		pad := ruleW - len(num) - lipgloss.Width(right) - 2
		if pad < 3 {
			pad = 3
		}
		lines = append(lines, styles.ModalHint.Render(num+strings.Repeat("─", pad))+right+styles.ModalHint.Render("──"))
		lines = append(lines, styles.ModalValue.Render(rec.Problem))
		if rec.Solution != "" && rec.Solution != rec.Problem {
			lines = append(lines, styles.AccentS.Render("→ ")+styles.ModalValue.Render(rec.Solution))
		}
		lines = append(lines, "")
	}

	if end < len(filt) {
		lines = append(lines, styles.ModalHint.Render(fmt.Sprintf("  ↓ %d more below", len(filt)-end)))
	}

	// Summary footer.
	lines = append(lines, styles.ModalHint.Render(strings.Repeat("─", ruleW)))
	lines = append(lines, styles.ModalLabel.Render(advisorSummary(filt)))
	lines = append(lines, "")
	lines = append(lines, styles.ModalHint.Render("↑↓ scroll  ·  esc  close"))
	return m.overlay(strings.Join(lines, "\n"))
}

// costWindowLine takes the plain MTD cost string stored on the node
// and appends the date window + method metadata so the advisor header
// matches what the portal shows: "GBP 5.60  2026-03-25 → 2026-04-24 ·
// ActualCost · pre-tax". Returns empty when the node has no cost.
func costWindowLine(cost string) string {
	cost = strings.TrimSpace(cost)
	if cost == "" {
		return ""
	}
	// Strip any trailing ↑/↓/→ arrow so the date range reads cleanly.
	for _, suffix := range []string{" ↑", " ↓", " →"} {
		if idx := strings.Index(cost, suffix); idx > 0 {
			cost = cost[:idx]
			break
		}
	}
	// 30-day rolling window ending today.
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	return fmt.Sprintf("%s  %s → %s · ActualCost · pre-tax",
		cost, from.Format("2006-01-02"), to.Format("2006-01-02"))
}

// resourceSummaryLine renders a single-line summary of a resource for
// the popup header — type, region, sku, cost, separated by · dots.
// Omits missing fields so AWS / GCP resources without sku still
// render cleanly.
func resourceSummaryLine(r provider.Node) string {
	parts := []string{}
	if t := r.Meta["type"]; t != "" {
		parts = append(parts, t)
	}
	switch {
	case parentRGName(r.ID) != "":
		parts = append(parts, parentRGName(r.ID))
	case r.Meta["project"] != "":
		parts = append(parts, r.Meta["project"])
	case r.Meta["accountId"] != "":
		parts = append(parts, r.Meta["accountId"])
	}
	if r.Location != "" {
		parts = append(parts, r.Location)
	}
	if sku := r.Meta["sku"]; sku != "" {
		parts = append(parts, sku)
	}
	if r.Cost != "" {
		parts = append(parts, r.Cost+"/30d")
	}
	return strings.Join(parts, " · ")
}

// impactBullet renders a coloured bullet for each impact level —
// visually encodes severity without taking a whole word of width in
// the card divider.
func impactBullet(impact string) string {
	switch strings.ToLower(impact) {
	case "high":
		return styles.Bad.Render("● HIGH")
	case "medium":
		return styles.WarnS.Render("● MEDIUM")
	case "low":
		return styles.Help.Render("● low")
	default:
		if impact == "" {
			return ""
		}
		return styles.Help.Render("● " + impact)
	}
}

// advisorSummary returns the "N recommendations · 2 medium · 1 high"
// footer line. Counts per impact so the user sees severity at a glance.
func advisorSummary(recs []provider.Recommendation) string {
	var high, med, low int
	for _, r := range recs {
		switch strings.ToLower(r.Impact) {
		case "high":
			high++
		case "medium":
			med++
		case "low":
			low++
		}
	}
	parts := []string{fmt.Sprintf("%d recommendations", len(recs))}
	if high > 0 {
		parts = append(parts, fmt.Sprintf("%d high", high))
	}
	if med > 0 {
		parts = append(parts, fmt.Sprintf("%d medium", med))
	}
	if low > 0 {
		parts = append(parts, fmt.Sprintf("%d low", low))
	}
	return strings.Join(parts, "  ·  ")
}

// advisorFullView keeps the original wide-scope table layout for sub /
// account / project scopes where there can be many recommendations
// spanning many resources.
func (m *model) advisorFullView(advisorName string, filt []provider.Recommendation) string {
	count := fmt.Sprintf("%d recommendation(s) for %s", len(m.advisorRecs), m.advisorName)
	if m.advisorFilter != "" {
		count = fmt.Sprintf("%d/%d for %s", len(filt), len(m.advisorRecs), m.advisorName)
	}
	header := styles.ModalTitle.Render(advisorName) + "  " + styles.Help.Render(count)
	box := fullScreenBox(m.width, m.height)
	if len(m.advisorRecs) == 0 {
		return box.Render(strings.Join([]string{
			header,
			"",
			"No recommendations at this scope.",
			"",
			styles.Help.Render(advisorEmptyHint(m.active)),
			styles.Help.Render("Drill further and press A again, or check the full report in the portal."),
			"",
			styles.Help.Render("esc close   o open in portal"),
		}, "\n"))
	}

	lines := []string{header, ""}
	if m.advisorFilterOn {
		lines = append(lines, m.advisorFilterIn.View(), "")
	} else if m.advisorFilter != "" {
		lines = append(lines, "  "+styles.Help.Render("filter: "+m.advisorFilter+"  (/ to change, esc in filter clears)"), "")
	}

	if len(filt) == 0 {
		lines = append(lines,
			styles.Help.Render("  no recommendations match the current filter"),
			"",
			styles.Help.Render("  /, type, esc   |   o portal   esc/A close"),
		)
		return box.Render(strings.Join(lines, "\n"))
	}

	max := len(filt)
	if max > 14 {
		max = 14
	}
	start := 0
	if m.advisorIdx >= max {
		start = m.advisorIdx - max + 1
	}
	for i := start; i < start+max && i < len(filt); i++ {
		r := filt[i]
		marker := "  "
		if i == m.advisorIdx {
			marker = "> "
		}
		line := fmt.Sprintf("%s%s  %s  %s  %s",
			marker,
			padRight(categoryBadge(r.Category), 14),
			padRight(impactBadge(r.Impact), 10),
			padRight(shortTail(r.ResourceID, 40), 40),
			shorten(r.Problem, 60),
		)
		if i == m.advisorIdx {
			line = styles.Selected.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "")

	if m.advisorIdx >= 0 && m.advisorIdx < len(filt) {
		r := filt[m.advisorIdx]
		lines = append(lines,
			styles.Header.Render("Details"),
			"Category: "+categoryBadge(r.Category)+"   Impact: "+impactBadge(r.Impact),
			"Target:   "+r.ResourceID,
			"Problem:  "+r.Problem,
			"Solution: "+r.Solution,
		)
		if r.LastUpdated != "" {
			lines = append(lines, "Updated:  "+shortDate(r.LastUpdated))
		}
	}
	lines = append(lines, "", styles.Help.Render("↑↓/jk move   / filter   o portal   esc/A close"))
	return box.Render(strings.Join(lines, "\n"))
}

// dominantCategory returns the subtitle shown after the advisor name
// (e.g. "cost recommendations") when every row shares the same
// category. Returns "" for mixed categories so we don't mislead.
func dominantCategory(recs []provider.Recommendation) string {
	if len(recs) == 0 {
		return ""
	}
	first := strings.ToLower(recs[0].Category)
	for _, r := range recs {
		if strings.ToLower(r.Category) != first {
			return ""
		}
	}
	return first
}
// advisorLabelFor returns the human name of the cloud's native
// recommender service. Drives the popup title so the user sees the
// proper product name ("Azure Advisor" / "Google Cloud Recommender" /
// "AWS Compute Optimizer") rather than a generic "Advisor".
func advisorLabelFor(p provider.Provider) string {
	if p == nil {
		return "Advisor"
	}
	switch p.Name() {
	case pimSrcAzure:
		return "Azure Advisor"
	case providerGCP:
		return "Google Cloud Recommender"
	case "aws":
		return "AWS Compute Optimizer"
	default:
		return p.Name() + " advisor"
	}
}

// advisorEmptyHint is the short explainer shown when the scope has
// zero recommendations. Tailored per cloud so the user knows what
// kind of suggestions would appear if there were any.
func advisorEmptyHint(p provider.Provider) string {
	if p == nil {
		return "Advisor generates cost / security / reliability / performance tips."
	}
	switch p.Name() {
	case pimSrcAzure:
		return "Azure Advisor surfaces cost, security, reliability, performance, and operational tips."
	case providerGCP:
		return "Google Cloud Recommender surfaces cost, performance, security, and reliability suggestions."
	case "aws":
		return "AWS Compute Optimizer + Trusted Advisor rightsizing and cost-efficiency suggestions."
	default:
		return "Advisor generates cost / security / reliability / performance tips."
	}
}

func categoryBadge(c string) string {
	switch strings.ToLower(c) {
	case "cost":
		return styles.Cost.Render("Cost")
	case "security":
		return styles.Bad.Render("Security")
	case "reliability", "highavailability", "high availability":
		return styles.WarnS.Render("Reliability")
	case "performance":
		return styles.AccentS.Render("Performance")
	case "operationalexcellence", "operational excellence":
		return styles.Help.Render("OpsExcellence")
	default:
		return c
	}
}

func impactBadge(i string) string {
	switch strings.ToLower(i) {
	case "high":
		return styles.Bad.Render("HIGH")
	case "medium":
		return styles.WarnS.Render("MED")
	case "low":
		return styles.Help.Render("low")
	default:
		return i
	}
}
