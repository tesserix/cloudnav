package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) toggleCost() tea.Cmd {
	m.showCost = !m.showCost
	if !m.showCost {
		m.status = "cost column off"
		m.refreshTable()
		return nil
	}
	if m.active == nil {
		m.status = "cost column on — enter a cloud first"
		m.refreshTable()
		return nil
	}
	scope, ok := m.costScope()
	if !ok {
		m.status = m.costHint()
		m.refreshTable()
		return nil
	}
	if kindOf(&m.stack[len(m.stack)-1]) == provider.KindSubscription {
		return m.loadSubscriptionCosts()
	}
	if m.atResourceLevel() {
		return m.loadResourceCosts()
	}
	coster, ok := m.active.(provider.Coster)
	if !ok {
		m.status = m.active.Name() + ": costs not supported yet"
		m.refreshTable()
		return nil
	}
	if _, cached := m.costs[scope.ID]; cached {
		m.refreshTable()
		m.status = statusCostCached
		return nil
	}
	m.loading = true
	m.status = "loading cost breakdown..."
	ctx := m.ctx
	scopeID := scope.ID
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			costs, err := coster.Costs(ctx, scope)
			if err != nil {
				return errMsg{err}
			}
			return costsLoadedMsg{parentID: scopeID, costs: costs}
		},
	)
}

func (m *model) loadAggregatedCost(top *frame) tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok {
		return nil
	}
	rgs := map[string]bool{}
	for _, n := range top.nodes {
		rgs[n.Meta["originRG"]] = true
	}
	cacheKey := "agg:" + top.title
	if _, cached := m.costs[cacheKey]; cached {
		return nil
	}
	// Find subscription — use one node's meta.
	subID := ""
	for _, n := range top.nodes {
		if s := n.Meta["subscriptionId"]; s != "" {
			subID = s
			break
		}
	}
	if subID == "" {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		merged := map[string]string{}
		for rg := range rgs {
			out, err := az.ResourceCosts(ctx, subID, rg)
			if err != nil {
				continue
			}
			for k, v := range out {
				merged[k] = v
			}
		}
		return costsLoadedMsg{parentID: cacheKey, costs: merged}
	}
}

func (m *model) loadResourceCosts() tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok {
		m.status = m.active.Name() + ": per-resource cost is Azure-only for now"
		m.refreshTable()
		return nil
	}
	top := &m.stack[len(m.stack)-1]
	if top.parent == nil || top.parent.Kind != provider.KindResourceGroup {
		return nil
	}
	rg := top.parent.Name
	subID := top.parent.Meta["subscriptionId"]
	if subID == "" && top.parent.Parent != nil {
		subID = top.parent.Parent.ID
	}
	if subID == "" {
		m.status = "resource cost: missing subscription context"
		m.refreshTable()
		return nil
	}
	cacheKey := "res:" + subID + "/" + rg
	if _, cached := m.costs[cacheKey]; cached {
		m.refreshTable()
		m.status = statusCostCached
		return nil
	}
	m.loading = true
	m.status = fmt.Sprintf("loading resource cost for %s...", rg)
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			costs, err := az.ResourceCosts(ctx, subID, rg)
			if err != nil {
				return errMsg{err}
			}
			return costsLoadedMsg{parentID: cacheKey, costs: costs}
		},
	)
}

func (m *model) loadSubscriptionCosts() tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok {
		m.status = m.active.Name() + ": subscription-level costs are Azure-only for now"
		m.refreshTable()
		return nil
	}
	if _, cached := m.costs["__azure_subs__"]; cached {
		m.refreshTable()
		m.status = statusCostCached
		return nil
	}
	top := m.stack[len(m.stack)-1]
	ids := make([]string, 0, len(top.nodes))
	for _, n := range top.nodes {
		ids = append(ids, n.ID)
	}
	m.loading = true
	m.status = fmt.Sprintf("loading cost for %d subscription(s)...", len(ids))
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			subCosts, _ := az.SubscriptionCosts(ctx, ids)
			out := make(map[string]string, len(subCosts))
			for _, c := range subCosts {
				if c.Error != "" {
					out[strings.ToLower(c.SubscriptionID)] = styles.Help.Render(c.Error)
					continue
				}
				out[strings.ToLower(c.SubscriptionID)] = formatSubCost(c.Current, c.LastMonth, c.Currency)
			}
			return costsLoadedMsg{parentID: "__azure_subs__", costs: out}
		},
	)
}

func formatSubCost(current, last float64, currency string) string {
	base := formatAmount(current, currency)
	if last == 0 && current == 0 {
		return base
	}
	if last == 0 {
		return base + " new"
	}
	delta := (current - last) / last * 100
	switch {
	case delta > 2:
		return fmt.Sprintf("%s ↑%d%%", base, int(delta+0.5))
	case delta < -2:
		return fmt.Sprintf("%s ↓%d%%", base, int(-delta+0.5))
	default:
		return base + " →"
	}
}

func formatAmount(amount float64, currency string) string {
	symbol := currencyChar(currency)
	return fmt.Sprintf("%s%.2f", symbol, amount)
}

func currencyChar(code string) string {
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
func (m *model) costScope() (provider.Node, bool) {
	top := &m.stack[len(m.stack)-1]
	switch kindOf(top) {
	case provider.KindSubscription:
		return provider.Node{ID: "__azure_subs__", Kind: provider.KindCloud}, true
	case provider.KindResourceGroup:
		if top.parent != nil && top.parent.Kind == provider.KindSubscription {
			return *top.parent, true
		}
	case provider.KindResource:
		if top.parent != nil && top.parent.Kind == provider.KindResourceGroup {
			return *top.parent, true
		}
	case provider.KindRegion:
		if top.parent != nil && top.parent.Kind == provider.KindAccount {
			return *top.parent, true
		}
	case provider.KindProject:
		return provider.Node{ID: providerGCP, Kind: provider.KindCloud}, true
	}
	return provider.Node{}, false
}

func (m *model) costHint() string {
	switch m.active.Name() {
	case pimSrcAzure:
		return "cost column on — drill into a subscription's resource groups"
	case "aws":
		return "cost column on — drill into the account's regions"
	case providerGCP:
		return "cost column on — press c on the projects list"
	default:
		return "cost column on — not supported at this view"
	}
}
func (m *model) mergeCosts(f *frame) {
	if !m.showCost {
		return
	}
	var costs map[string]string
	if f.aggregated {
		costs = m.costs["agg:"+f.title]
		for i, n := range m.visibleNodes {
			if c, ok := costs[strings.ToLower(n.ID)]; ok {
				m.visibleNodes[i].Cost = c
			}
		}
		return
	}
	switch kindOf(f) {
	case provider.KindSubscription:
		costs = m.costs["__azure_subs__"]
	case provider.KindResourceGroup:
		if f.parent != nil {
			costs = m.costs[f.parent.ID]
		}
	case provider.KindResource:
		if f.parent != nil && f.parent.Parent != nil {
			subID := f.parent.Meta["subscriptionId"]
			if subID == "" {
				subID = f.parent.Parent.ID
			}
			costs = m.costs["res:"+subID+"/"+f.parent.Name]
		}
	case provider.KindRegion:
		if f.parent != nil {
			costs = m.costs[f.parent.ID]
		}
	case provider.KindProject:
		costs = m.costs["gcp"]
	}
	if costs == nil {
		return
	}
	for i, n := range m.visibleNodes {
		for _, key := range []string{
			strings.ToLower(n.ID),
			strings.ToLower(n.Name),
		} {
			if c, ok := costs[key]; ok {
				m.visibleNodes[i].Cost = c
				break
			}
		}
	}
}
func (m *model) maybeAutoLoadCost() tea.Cmd {
	if !m.showCost {
		return nil
	}
	if len(m.stack) == 0 {
		return nil
	}
	top := &m.stack[len(m.stack)-1]
	if top.aggregated {
		return m.loadAggregatedCost(top)
	}
	scope, ok := m.costScope()
	if !ok {
		return nil
	}
	cacheKey := scope.ID
	if m.atResourceLevel() && scope.Kind == provider.KindResourceGroup {
		subID := scope.Meta["subscriptionId"]
		if subID == "" && scope.Parent != nil {
			subID = scope.Parent.ID
		}
		if subID == "" {
			return nil
		}
		cacheKey = "res:" + subID + "/" + scope.Name
	}
	if m.atSubscriptionLevel() {
		cacheKey = "__azure_subs__"
	}
	if m.atRGLevel() && scope.Kind == provider.KindSubscription {
		cacheKey = scope.ID
	}
	if _, cached := m.costs[cacheKey]; cached {
		return nil
	}
	return m.toggleCostInner()
}

// toggleCostInner fires the same load paths as the <c> keybinding without
// flipping the showCost flag.
func (m *model) toggleCostInner() tea.Cmd {
	if m.atSubscriptionLevel() {
		return m.loadSubscriptionCosts()
	}
	if m.atResourceLevel() {
		return m.loadResourceCosts()
	}
	coster, ok := m.active.(provider.Coster)
	if !ok {
		return nil
	}
	scope, ok := m.costScope()
	if !ok {
		return nil
	}
	if _, cached := m.costs[scope.ID]; cached {
		return nil
	}
	ctx := m.ctx
	scopeID := scope.ID
	return func() tea.Msg {
		costs, err := coster.Costs(ctx, scope)
		if err != nil {
			return costsLoadedMsg{parentID: scopeID, costs: nil}
		}
		return costsLoadedMsg{parentID: scopeID, costs: costs}
	}
}
