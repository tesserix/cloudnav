package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
)

func (m *model) drillDown() tea.Cmd {
	if m.atRGLevel() && len(m.selected) > 0 {
		return m.drillAggregated()
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	cur := m.visibleNodes[c]
	switch cur.Kind {
	case provider.KindCloud:
		for _, p := range m.providers {
			if p.Name() == cur.Name {
				m.active = p
				m.resetView()
				return m.load(p.Name(), nil)
			}
		}
	case provider.KindCloudDisabled:
		m.status = "coming soon"
	case provider.KindSubscription,
		provider.KindResourceGroup,
		provider.KindProject,
		provider.KindAccount,
		provider.KindRegion:
		m.resetView()
		return m.load(cur.Name, &cur)
	}
	return nil
}

func (m *model) drillAggregated() tea.Cmd {
	selected := make([]provider.Node, 0, len(m.selected))
	for _, n := range m.visibleNodes {
		if m.selected[n.ID] {
			selected = append(selected, n)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	prov := m.active
	ctx := m.ctx
	m.loading = true
	m.drilling = true
	m.status = fmt.Sprintf("loading resources across %d resource group(s)...", len(selected))

	// Fast path: Azure Resource Graph returns resources for all selected
	// RGs in one KQL call. Replaces N sequential `az resource list`
	// fanouts that otherwise made a 10-RG drill take 10-30s.
	if az, ok := prov.(*azure.Azure); ok {
		subID := selected[0].Meta["subscriptionId"]
		if subID == "" && selected[0].Parent != nil {
			subID = selected[0].Parent.ID
		}
		if subID != "" {
			rgNames := make([]string, 0, len(selected))
			for _, n := range selected {
				rgNames = append(rgNames, n.Name)
			}
			return tea.Batch(
				m.spinner.Tick,
				func() tea.Msg {
					nodes, err := az.ResourcesInRGs(ctx, subID, rgNames)
					if err != nil {
						// Fallback to per-RG fanout when Resource Graph
						// fails (e.g. user lacks RG reader at the
						// subscription level).
						return nodesLoadedMsg{frame: aggregateFromChildren(ctx, prov, selected)}
					}
					// Stamp originRG so the cost merge can bucket rows
					// back by their source RG.
					nodes = tagOriginRG(nodes)
					return nodesLoadedMsg{frame: frame{
						title:      fmt.Sprintf("%d resource group(s)", len(selected)),
						nodes:      nodes,
						aggregated: true,
					}}
				},
			)
		}
	}

	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			return nodesLoadedMsg{frame: aggregateFromChildren(ctx, prov, selected)}
		},
	)
}

// aggregateFromChildren walks the provider's Children() API per RG — the
// fallback when Resource Graph isn't available (non-Azure providers or a
// KQL failure). Same semantics as before the Resource Graph fast path.
func aggregateFromChildren(ctx context.Context, prov provider.Provider, selected []provider.Node) frame {
	var combined []provider.Node
	for _, rg := range selected {
		rg := rg
		nodes, err := prov.Children(ctx, rg)
		if err != nil {
			continue
		}
		for i := range nodes {
			if nodes[i].Meta == nil {
				nodes[i].Meta = map[string]string{}
			}
			nodes[i].Meta["originRG"] = rg.Name
		}
		combined = append(combined, nodes...)
	}
	return frame{
		title:      fmt.Sprintf("%d resource group(s)", len(selected)),
		nodes:      combined,
		aggregated: true,
	}
}

// tagOriginRG derives each resource's origin RG from its ID and stamps
// Meta["originRG"] so the aggregated cost merge can find it. Resource
// Graph returns the RG name as a field, which we surface via parentRGName.
func tagOriginRG(nodes []provider.Node) []provider.Node {
	for i := range nodes {
		if nodes[i].Meta == nil {
			nodes[i].Meta = map[string]string{}
		}
		if nodes[i].Meta["originRG"] == "" {
			nodes[i].Meta["originRG"] = parentRGName(nodes[i].ID)
		}
	}
	return nodes
}

func (m *model) resetView() {
	m.filter = ""
	m.search.SetValue("")
	m.searchMode = false
	m.search.Blur()
	m.tenantFilter = ""
	m.categoryFilter = ""
}

func (m *model) goBack() tea.Cmd {
	if len(m.stack) <= 1 {
		return tea.Quit
	}
	m.stack = m.stack[:len(m.stack)-1]
	if len(m.stack) == 1 {
		m.active = nil
	}
	m.resetView()
	m.refreshTable()
	m.table.SetCursor(0)
	m.status = ""
	return nil
}

func (m *model) reload() tea.Cmd {
	if len(m.stack) <= 1 || m.active == nil {
		return nil
	}
	// If the user hits refresh at the subscription list, they're asking
	// for fresh data — bypass the provider's short-lived Root() cache.
	if az, ok := m.active.(*azure.Azure); ok {
		az.InvalidateRootCache()
	}
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	// Quiet reload: user is already looking at this level, so keep the
	// table visible and show progress only in the footer spinner. A
	// drill load (drillLoadingBody) would flash-replace the table which
	// reads as "jumbled".
	return m.loadInto(top.title, top.parent, false)
}

func (m *model) load(title string, parent *provider.Node) tea.Cmd {
	return m.loadInto(title, parent, true)
}

// loadInto is the shared load path. drill=true swaps the table for the
// full-screen loading panel (used for fresh drill-downs where the user
// has no context yet). drill=false keeps the current table visible and
// only shows the footer spinner — appropriate for refresh / reload
// after a delete.
func (m *model) loadInto(title string, parent *provider.Node, drill bool) tea.Cmd {
	if m.active == nil {
		return nil
	}
	m.loading = true
	m.drilling = drill
	m.err = nil
	m.status = "loading " + title + "..."
	prov := m.active
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			var (
				nodes []provider.Node
				err   error
			)
			if parent == nil {
				nodes, err = prov.Root(ctx)
			} else {
				nodes, err = prov.Children(ctx, *parent)
			}
			if err != nil {
				return errMsg{err}
			}
			return nodesLoadedMsg{frame: frame{title: title, parent: parent, nodes: nodes}}
		},
	)
}
