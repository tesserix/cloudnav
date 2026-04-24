package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) loadPIM() tea.Cmd {
	if m.active == nil {
		m.status = "enter a cloud first (↵ on a cloud row) before requesting PIM roles"
		return nil
	}
	if _, ok := m.active.(provider.PIMer); !ok {
		m.status = m.active.Name() + ": JIT elevation not supported yet (planned — use Azure for now)"
		return nil
	}
	// Disk cache hit: serve instantly. PIM list is expensive — for a
	// user with N tenants it's N × 2 token acquisitions (each spawning
	// az under the hood via AzureCLICredential) plus N × ~3 HTTP calls.
	// A 5-min TTL means re-opening PIM within a work block is instant
	// while still catching newly-granted eligibilities reasonably fast.
	if roles, ok := m.warmPIMCache(); ok {
		m.pimRoles = roles
		m.pimCursor = 0
		m.pimMode = true
		m.status = fmt.Sprintf("%d eligible role(s) (cached)", len(roles))
		return nil
	}
	m.loading = true
	m.status = "loading PIM eligible roles..."
	prov := m.active.(provider.PIMer)
	ctx := m.ctx
	cache := m.pimCache
	cacheKey := m.active.Name() + ":roles"
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			roles, err := prov.ListEligibleRoles(ctx)
			if err != nil {
				return errMsg{err}
			}
			if cache != nil && len(roles) > 0 {
				_ = cache.Set(cacheKey, roles)
			}
			return pimLoadedMsg{roles: roles}
		},
	)
}

// warmPIMCache reads from the disk cache. Returns the cached roles
// when still fresh, (nil, false) otherwise.
func (m *model) warmPIMCache() ([]provider.PIMRole, bool) {
	if m.pimCache == nil || m.active == nil {
		return nil, false
	}
	return m.pimCache.Get(m.active.Name() + ":roles")
}

func (m *model) updatePIM(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pimActivate {
		return m.updatePIMActivate(msg)
	}
	if m.pimFilterOn {
		return m.updatePIMFilter(msg)
	}
	switch msg.String() {
	case keyEsc, "q":
		m.pimMode = false
		m.status = ""
		return m, nil
	case keyUp, "k":
		if m.pimCursor > 0 {
			m.pimCursor--
			m.syncPIMDurationToPolicy()
		}
		return m, nil
	case keyDown, "j":
		if m.pimCursor < len(m.filteredPIM())-1 {
			m.pimCursor++
			m.syncPIMDurationToPolicy()
		}
		return m, nil
	case "/":
		m.pimFilterOn = true
		m.pimFilterIn.SetValue(m.pimFilter)
		m.pimFilterIn.Focus()
		return m, nil
	case "a", keyEnter:
		if len(m.filteredPIM()) == 0 {
			return m, nil
		}
		m.pimActivate = true
		m.pimInput.SetValue("")
		m.pimInput.Focus()
		return m, nil
	case "+":
		if m.pimDuration < m.pimDurationCap() {
			m.pimDuration++
		}
		return m, nil
	case "-":
		if m.pimDuration > 1 {
			m.pimDuration--
		}
		return m, nil
	case "0":
		m.pimSourceFilt = ""
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case "1":
		m.pimSourceFilt = pimSrcAzure
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case "2":
		m.pimSourceFilt = pimSrcEntra
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case "3":
		m.pimSourceFilt = pimSrcGroup
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case "4":
		m.pimSourceFilt = pimSrcGCP
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case "5":
		m.pimSourceFilt = pimSrcAWSSSO
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	}
	return m, nil
}

func (m *model) updatePIMFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.pimFilterOn = false
		m.pimFilter = ""
		m.pimFilterIn.SetValue("")
		m.pimFilterIn.Blur()
		m.pimCursor = 0
		m.syncPIMDurationToPolicy()
		return m, nil
	case keyEnter:
		m.pimFilterOn = false
		m.pimFilterIn.Blur()
		m.syncPIMDurationToPolicy()
		return m, nil
	case keyUp, keyDown:
		if msg.String() == keyUp && m.pimCursor > 0 {
			m.pimCursor--
		}
		if msg.String() == keyDown && m.pimCursor < len(m.filteredPIM())-1 {
			m.pimCursor++
		}
		m.syncPIMDurationToPolicy()
		return m, nil
	}
	var cmd tea.Cmd
	m.pimFilterIn, cmd = m.pimFilterIn.Update(msg)
	m.pimFilter = m.pimFilterIn.Value()
	if m.pimCursor >= len(m.filteredPIM()) {
		m.pimCursor = 0
	}
	m.syncPIMDurationToPolicy()
	return m, cmd
}

// syncPIMDurationToPolicy sets pimDuration to the role's policy-defined max
// when known, so + / - starts from the Azure-configured ceiling. Falls back
// to 8h when the policy is unreadable.
func (m *model) syncPIMDurationToPolicy() {
	filt := m.filteredPIM()
	if len(filt) == 0 || m.pimCursor >= len(filt) {
		return
	}
	role := filt[m.pimCursor]
	if role.MaxDurationHours > 0 {
		m.pimDuration = role.MaxDurationHours
		return
	}
	if m.pimDuration <= 0 {
		m.pimDuration = 8
	}
}

// pimDurationCap returns the upper bound for the duration stepper — the
// current role's policy max when known, else 24h.
func (m *model) pimDurationCap() int {
	filt := m.filteredPIM()
	if len(filt) == 0 || m.pimCursor >= len(filt) {
		return 24
	}
	if max := filt[m.pimCursor].MaxDurationHours; max > 0 {
		return max
	}
	return 24
}

func (m *model) filteredPIM() []provider.PIMRole {
	q := strings.ToLower(m.pimFilter)
	src := m.pimSourceFilt
	out := make([]provider.PIMRole, 0, len(m.pimRoles))
	for _, r := range m.pimRoles {
		if src != "" && r.Source != src {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(r.RoleName), q) &&
			!strings.Contains(strings.ToLower(r.ScopeName), q) &&
			!strings.Contains(strings.ToLower(r.Scope), q) &&
			!strings.Contains(strings.ToLower(r.TenantID), q) &&
			!strings.Contains(strings.ToLower(r.Source), q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// pimSourceCounts returns { "azure": N, "entra": M, "group": K } so the tab
// bar can show totals per source in the header.
func (m *model) pimSourceCounts() map[string]int {
	out := map[string]int{}
	for _, r := range m.pimRoles {
		src := r.Source
		if src == "" {
			src = pimSrcAzure
		}
		out[src]++
	}
	return out
}

func (m *model) updatePIMActivate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.pimActivate = false
		m.pimInput.Blur()
		return m, nil
	case keyEnter:
		reason := strings.TrimSpace(m.pimInput.Value())
		if reason == "" {
			m.status = "justification is required"
			return m, nil
		}
		filt := m.filteredPIM()
		if len(filt) == 0 || m.pimCursor >= len(filt) {
			return m, nil
		}
		role := filt[m.pimCursor]
		if role.Active {
			m.pimActivate = false
			m.pimInput.Blur()
			m.status = fmt.Sprintf("%s on %s is already ACTIVE until %s — nothing to do", role.RoleName, scopeDisplay(role), humanUntil(role.ActiveUntil))
			return m, nil
		}
		m.pimActivate = false
		m.pimInput.Blur()
		m.loading = true
		m.status = fmt.Sprintf("activating %s on %s for %dh...", role.RoleName, scopeDisplay(role), m.pimDuration)
		prov := m.active.(provider.PIMer)
		ctx := m.ctx
		dur := m.pimDuration
		expires := time.Now().Add(time.Duration(dur) * time.Hour).UTC().Format(time.RFC3339)
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				err := prov.ActivateRole(ctx, role, reason, dur)
				return pimActivatedMsg{
					role:      role.RoleName + " on " + scopeDisplay(role),
					roleID:    role.ID,
					expiresAt: expires,
					err:       err,
				}
			},
		)
	}
	var cmd tea.Cmd
	m.pimInput, cmd = m.pimInput.Update(msg)
	return m, cmd
}

func humanUntil(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		if iso == "" {
			return "expiry unknown"
		}
		return iso
	}
	rem := time.Until(t)
	if rem <= 0 {
		return "just expired"
	}
	local := t.Local().Format("15:04 Jan-02")
	return fmt.Sprintf("%s (%s left)", local, humanDuration(rem))
}

func humanDuration(d time.Duration) string {
	if d >= time.Hour {
		h := int(d / time.Hour)
		m := int(d%time.Hour) / int(time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

func scopeDisplay(r provider.PIMRole) string {
	if r.ScopeName != "" {
		return r.ScopeName
	}
	return r.Scope
}

func pimSourceBadge(src string) string {
	switch src {
	case pimSrcEntra:
		return styles.AccentS.Render("entra")
	case pimSrcGroup:
		return styles.WarnS.Render("group")
	case pimSrcGCP:
		return styles.Good.Render("gcp-pam")
	case pimSrcAWSSSO:
		return styles.AccentS.Render("aws-sso")
	case pimSrcAzure, "":
		return styles.Help.Render(pimSrcAzure)
	default:
		return styles.Help.Render(src)
	}
}

// pimSourceLabel is the raw (unstyled) form of pimSourceBadge, for contexts
// where an outer row style (e.g. the Selected highlight) needs to span the
// badge without being broken by the badge's own ANSI reset.
func pimSourceLabel(src string) string {
	switch src {
	case pimSrcEntra:
		return "entra"
	case pimSrcGroup:
		return "group"
	case pimSrcGCP:
		return "gcp-pam"
	case pimSrcAWSSSO:
		return "aws-sso"
	case pimSrcAzure, "":
		return pimSrcAzure
	default:
		return src
	}
}

func (m *model) pimView() string {
	filt := m.filteredPIM()
	headerCount := fmt.Sprintf("%d role(s)", len(m.pimRoles))
	if m.pimFilter != "" {
		headerCount = fmt.Sprintf("%d/%d", len(filt), len(m.pimRoles))
	}
	durHint := fmt.Sprintf("duration %dh", m.pimDuration)
	if len(filt) > 0 && m.pimCursor < len(filt) {
		if max := filt[m.pimCursor].MaxDurationHours; max > 0 {
			durHint = fmt.Sprintf("duration %dh (policy max %dh)", m.pimDuration, max)
		} else {
			durHint = fmt.Sprintf("duration %dh (policy not readable, default 8h)", m.pimDuration)
		}
	}
	counts := m.pimSourceCounts()
	tab := func(key, label, src string, n int) string {
		text := fmt.Sprintf("%s %s (%d)", key, label, n)
		if m.pimSourceFilt == src {
			return styles.Selected.Render(" " + text + " ")
		}
		return styles.Help.Render(" " + text + " ")
	}
	tabs := strings.Join([]string{
		tab("0", "all", "", len(m.pimRoles)),
		tab("1", "Azure", pimSrcAzure, counts[pimSrcAzure]),
		tab("2", "Entra", pimSrcEntra, counts[pimSrcEntra]),
		tab("3", "Groups", pimSrcGroup, counts[pimSrcGroup]),
		tab("4", "GCP PAM", pimSrcGCP, counts[pimSrcGCP]),
		tab("5", "AWS SSO", pimSrcAWSSSO, counts[pimSrcAWSSSO]),
	}, "")
	lines := []string{
		styles.Title.Render("PIM eligible roles") + "  " +
			styles.Help.Render(fmt.Sprintf("%s  %s (use +/- to change)", headerCount, durHint)),
		tabs,
		"",
	}
	if m.pimFilterOn {
		lines = append(lines, m.pimFilterIn.View(), "")
	} else if m.pimFilter != "" {
		lines = append(lines, "  "+styles.Help.Render("filter: "+m.pimFilter+"  (/ to change, esc in filter clears)"), "")
	}
	if len(filt) == 0 {
		if len(m.pimRoles) > 0 && m.pimFilter != "" {
			lines = append(lines,
				styles.Help.Render("  no roles match the current filter"),
			)
		} else {
			lines = append(lines,
				styles.Help.Render("  no eligible PIM assignments for this user"),
				"",
				styles.Help.Render("  if you expect some, check:"),
				styles.Help.Render("    • PIM is enabled on the tenant"),
				styles.Help.Render("    • you have read on roleEligibilityScheduleInstances"),
			)
		}
	}
	window := 14
	if m.height > 12 {
		window = m.height - 12
	}
	if window < 5 {
		window = 5
	}
	start := 0
	if len(filt) > window {
		start = m.pimCursor - window/2
		if start < 0 {
			start = 0
		}
		if start+window > len(filt) {
			start = len(filt) - window
		}
	}
	end := start + window
	if end > len(filt) {
		end = len(filt)
	}
	if start > 0 {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↑ %d more above", start)))
	}
	for i := start; i < end; i++ {
		r := filt[i]
		state := ""
		if r.Active {
			state = "  " + styles.Good.Render("● ACTIVE until "+humanUntil(r.ActiveUntil))
		}
		// For the selected row, use the plain source label so lipgloss's
		// Selected background spans the full line; the badge's own ANSI
		// reset would otherwise terminate the highlight mid-row.
		var src string
		if i == m.pimCursor {
			src = padRight(pimSourceLabel(r.Source), 8)
		} else {
			src = padRight(pimSourceBadge(r.Source), 8)
		}
		rowText := fmt.Sprintf("%2d. %s %-36s  on  %-30s", i+1, src, shorten(r.RoleName, 36), shorten(scopeDisplay(r), 30))
		if i == m.pimCursor {
			lines = append(lines, styles.Selected.Render("> "+rowText)+state)
		} else {
			lines = append(lines, "  "+rowText+state)
		}
	}
	if end < len(filt) {
		lines = append(lines, styles.Help.Render(fmt.Sprintf("  ↓ %d more below", len(filt)-end)))
	}
	switch {
	case m.pimActivate && len(filt) > 0:
		role := filt[m.pimCursor]
		lines = append(lines,
			"",
			styles.Help.Render("activate: ")+role.RoleName+"  on  "+scopeDisplay(role)+fmt.Sprintf("  for %dh", m.pimDuration),
			m.pimInput.View(),
			styles.Help.Render("enter submit  esc cancel"),
		)
	case m.loading:
		lines = append(lines,
			"",
			"  "+m.spinner.View()+" "+styles.Help.Render(m.status),
		)
	case m.err != nil:
		lines = append(lines,
			"",
			"  "+styles.Bad.Render("error: ")+firstErrLine(m.err),
			styles.Help.Render("  esc to close, a to retry"),
		)
	case m.status != "":
		lines = append(lines,
			"",
			"  "+styles.Good.Render(m.status),
			styles.Help.Render("  PIM activations can take up to a minute to become effective in Azure"),
			styles.Help.Render("  ↑↓ / jk move  a activate  +/- duration  0/1/2/3 source  esc close"),
		)
	default:
		lines = append(lines,
			"",
			styles.Help.Render("  ↑↓ / jk move  / filter  a activate  +/- duration  esc close"),
		)
	}
	return fullScreenBox(m.width, m.height).Render(strings.Join(lines, "\n"))
}
