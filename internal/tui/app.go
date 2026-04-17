// Package tui hosts the Bubbletea application. It is deliberately cloud-agnostic:
// every cloud concept arrives through a provider.Provider and is rendered via
// generic table rows. Adding a new cloud means implementing the provider
// interface — not editing this package.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/config"
	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
	"github.com/tesserix/cloudnav/internal/tui/keys"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

const (
	keyEsc   = "esc"
	keyEnter = "enter"
)

func Run() error {
	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type frame struct {
	title  string
	parent *provider.Node
	nodes  []provider.Node
}

type (
	nodesLoadedMsg  struct{ frame frame }
	detailLoadedMsg struct {
		title string
		body  string
	}
	costsLoadedMsg struct {
		parentID string
		costs    map[string]string
	}
	entitiesLoadedMsg struct {
		provider string
		nodes    []provider.Node
	}
	pimLoadedMsg    struct{ roles []provider.PIMRole }
	pimActivatedMsg struct {
		role string
		err  error
	}
	errMsg struct{ err error }
)

type sortMode int

const (
	sortName sortMode = iota
	sortState
	sortLocation
)

func (s sortMode) String() string {
	switch s {
	case sortState:
		return "state"
	case sortLocation:
		return "location"
	default:
		return "name"
	}
}

type paletteItem struct {
	label    string
	action   string // "switch-cloud" | "open-bookmark" | "jump-entity"
	arg      string
	provider string        // owning provider for jump-entity
	node     provider.Node // target for jump-entity
}

type model struct {
	ctx          context.Context
	providers    []provider.Provider
	active       provider.Provider
	stack        []frame
	visibleNodes []provider.Node
	table        table.Model
	spinner      spinner.Model
	search       textinput.Model
	detail       viewport.Model
	detailTitle  string
	detailMode   bool
	searchMode   bool
	filter       string
	sort         sortMode
	loading      bool
	err          error
	status       string
	showHelp     bool
	paletteMode  bool
	paletteInput textinput.Model
	paletteItems []paletteItem
	paletteIdx   int
	cfg          *config.Config
	showCost     bool
	costs        map[string]map[string]string // subID → lowercased rg name → cost
	restorePath  []config.Crumb               // remaining crumbs to drill into during bookmark restore
	restoreLabel string                       // label shown while restoring (for status)
	entities     map[string][]provider.Node   // provider name → top-level entities (subs/projects/accounts)
	pimMode      bool
	pimRoles     []provider.PIMRole
	pimCursor    int
	pimActivate  bool
	pimInput     textinput.Model
	pimDuration  int
	width        int
	height       int
	keys         keys.Map
}

func newModel() *model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(styles.Cyan)

	t := table.New(
		table.WithFocused(true),
		table.WithHeight(20),
	)
	ts := table.DefaultStyles()
	ts.Header = ts.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(styles.Subtle).
		BorderBottom(true).
		Bold(true).
		Foreground(styles.Fg)
	ts.Selected = ts.Selected.
		Background(styles.Purple).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)
	t.SetStyles(ts)

	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter by name"
	ti.CharLimit = 120
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.Cyan).Bold(true)

	pi := textinput.New()
	pi.Prompt = ": "
	pi.Placeholder = "search any sub / project / account, or switch cloud, or jump to bookmark"
	pi.CharLimit = 120
	pi.PromptStyle = lipgloss.NewStyle().Foreground(styles.Cyan).Bold(true)

	pimIn := textinput.New()
	pimIn.Prompt = "justification: "
	pimIn.Placeholder = "e.g. investigating prod incident INC-4812"
	pimIn.CharLimit = 200
	pimIn.PromptStyle = lipgloss.NewStyle().Foreground(styles.Cyan).Bold(true)

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle()

	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}

	m := &model{
		ctx:          context.Background(),
		providers:    []provider.Provider{azure.New(), gcp.New(), aws.New()},
		spinner:      sp,
		search:       ti,
		paletteInput: pi,
		pimInput:     pimIn,
		pimDuration:  1,
		detail:       vp,
		cfg:          cfg,
		costs:        map[string]map[string]string{},
		entities:     map[string][]provider.Node{},
		keys:         keys.Default(),
		table:        t,
	}
	m.pushHome()
	return m
}

func (m *model) pushHome() {
	home := frame{title: "clouds"}
	for _, p := range m.providers {
		home.nodes = append(home.nodes, provider.Node{
			Name: p.Name(),
			Kind: provider.KindCloud,
		})
	}
	m.stack = []frame{home}
	m.refreshTable()
}

func (m *model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if w := msg.Width - 2; w > 0 {
			m.table.SetWidth(w)
			m.search.Width = w - 4
			m.detail.Width = w
		}
		if h := msg.Height - 4; h > 0 {
			m.table.SetHeight(h)
			m.detail.Height = h
		}
		m.refreshTable()
		return m, nil

	case tea.KeyMsg:
		if m.pimMode {
			return m.updatePIM(msg)
		}
		if m.detailMode {
			if msg.String() == keyEsc || msg.String() == "q" {
				m.detailMode = false
				return m, nil
			}
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		}
		if m.paletteMode {
			return m.updatePalette(msg)
		}
		if m.searchMode {
			return m.updateSearch(msg)
		}
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.showHelp = true
			return m, nil
		case key.Matches(msg, m.keys.Search):
			m.searchMode = true
			m.search.Focus()
			return m, nil
		case key.Matches(msg, m.keys.Palette):
			return m, m.openPalette()
		case key.Matches(msg, m.keys.Flag):
			m.saveBookmark()
			return m, nil
		case key.Matches(msg, m.keys.Sort):
			m.sort = (m.sort + 1) % 3
			m.refreshTable()
			m.status = "sort: " + m.sort.String()
			return m, nil
		case key.Matches(msg, m.keys.Costs):
			return m, m.toggleCost()
		case key.Matches(msg, m.keys.Detail):
			return m, m.loadDetail()
		case key.Matches(msg, m.keys.PIM):
			return m, m.loadPIM()
		case key.Matches(msg, m.keys.Exec):
			return m, m.execShell()
		case key.Matches(msg, m.keys.Enter):
			return m, m.drillDown()
		case key.Matches(msg, m.keys.Back):
			return m, m.goBack()
		case key.Matches(msg, m.keys.Refresh):
			return m, m.reload()
		case key.Matches(msg, m.keys.Portal):
			m.openPortal()
			return m, nil
		}

	case nodesLoadedMsg:
		m.loading = false
		m.err = nil
		m.stack = append(m.stack, msg.frame)
		m.refreshTable()
		m.table.SetCursor(0)
		m.status = fmt.Sprintf("%d items", len(msg.frame.nodes))
		if cmd := m.advanceRestore(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case detailLoadedMsg:
		m.loading = false
		m.err = nil
		m.detailTitle = msg.title
		m.detail.SetContent(msg.body)
		m.detail.GotoTop()
		m.detailMode = true
		m.status = ""
		return m, nil

	case costsLoadedMsg:
		m.loading = false
		m.err = nil
		m.costs[msg.parentID] = msg.costs
		m.refreshTable()
		m.status = fmt.Sprintf("costs: %d RGs", len(msg.costs))
		return m, nil

	case entitiesLoadedMsg:
		m.entities[msg.provider] = msg.nodes
		if m.paletteMode {
			m.rebuildPalette()
		}
		return m, nil

	case pimLoadedMsg:
		m.loading = false
		m.err = nil
		m.pimRoles = msg.roles
		m.pimCursor = 0
		m.pimMode = true
		m.pimActivate = false
		m.status = fmt.Sprintf("%d eligible role assignment(s)", len(msg.roles))
		return m, nil

	case pimActivatedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.status = ""
			return m, nil
		}
		m.err = nil
		m.status = "✓ activation requested for " + msg.role
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg.err
		m.status = ""
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) openPalette() tea.Cmd {
	m.paletteMode = true
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	m.paletteIdx = 0
	m.rebuildPalette()
	return m.preloadEntities()
}

func (m *model) preloadEntities() tea.Cmd {
	cmds := []tea.Cmd{}
	for _, p := range m.providers {
		if _, ok := m.entities[p.Name()]; ok {
			continue
		}
		prov := p
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg {
			nodes, err := prov.Root(ctx)
			if err != nil {
				return entitiesLoadedMsg{provider: prov.Name(), nodes: nil}
			}
			return entitiesLoadedMsg{provider: prov.Name(), nodes: nodes}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *model) rebuildPalette() {
	q := strings.ToLower(m.paletteInput.Value())
	all := make([]paletteItem, 0, 32)

	for _, p := range m.providers {
		all = append(all, paletteItem{
			label:  "☁  switch to " + p.Name(),
			action: "switch-cloud",
			arg:    p.Name(),
		})
	}
	for _, bm := range m.cfg.Bookmarks {
		all = append(all, paletteItem{
			label:  "★ " + bm.Label,
			action: "open-bookmark",
			arg:    bm.Label,
		})
	}
	for _, p := range m.providers {
		for _, n := range m.entities[p.Name()] {
			all = append(all, paletteItem{
				label:    "▸ " + p.Name() + "  " + n.Name + "  " + shortID(n.ID),
				action:   "jump-entity",
				provider: p.Name(),
				node:     n,
			})
		}
	}

	if q == "" {
		m.paletteItems = all
	} else {
		filtered := make([]paletteItem, 0, len(all))
		for _, it := range all {
			if containsFold(it.label, q) || containsFold(it.arg, q) || containsFold(it.node.ID, q) {
				filtered = append(filtered, it)
			}
		}
		m.paletteItems = filtered
	}
	if m.paletteIdx >= len(m.paletteItems) {
		m.paletteIdx = 0
	}
}

func containsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), needle)
}

func (m *model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.paletteMode = false
		m.paletteInput.Blur()
		return m, nil
	case "up":
		if m.paletteIdx > 0 {
			m.paletteIdx--
		}
		return m, nil
	case "down":
		if m.paletteIdx < len(m.paletteItems)-1 {
			m.paletteIdx++
		}
		return m, nil
	case keyEnter:
		if m.paletteIdx < len(m.paletteItems) {
			cmd := m.runPaletteItem(m.paletteItems[m.paletteIdx])
			m.paletteMode = false
			m.paletteInput.Blur()
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.rebuildPalette()
	return m, cmd
}

func (m *model) runPaletteItem(it paletteItem) tea.Cmd {
	switch it.action {
	case "switch-cloud":
		for _, p := range m.providers {
			if p.Name() == it.arg {
				m.active = p
				m.resetView()
				m.stack = m.stack[:1]
				return m.load(p.Name(), nil)
			}
		}
	case "open-bookmark":
		for _, bm := range m.cfg.Bookmarks {
			if bm.Label == it.arg {
				return m.openBookmark(bm)
			}
		}
	case "jump-entity":
		for _, p := range m.providers {
			if p.Name() != it.provider {
				continue
			}
			m.active = p
			m.resetView()
			m.stack = m.stack[:1]
			m.restorePath = []config.Crumb{{
				Kind: string(it.node.Kind),
				ID:   it.node.ID,
				Name: it.node.Name,
			}}
			m.restoreLabel = p.Name() + " / " + it.node.Name
			m.status = "jumping to " + m.restoreLabel + "..."
			return m.load(p.Name(), nil)
		}
	}
	return nil
}

func (m *model) openBookmark(bm config.Bookmark) tea.Cmd {
	for _, p := range m.providers {
		if p.Name() == bm.Provider {
			m.active = p
			m.resetView()
			m.stack = m.stack[:1]
			// Skip the first crumb — it's the cloud level we just set as active.
			if len(bm.Path) > 1 {
				m.restorePath = append(m.restorePath[:0], bm.Path[1:]...)
				m.restoreLabel = bm.Label
				m.status = "restoring ★ " + bm.Label + "..."
			} else {
				m.restorePath = nil
				m.restoreLabel = ""
				m.status = "★ " + bm.Label
			}
			return m.load(p.Name(), nil)
		}
	}
	m.status = "bookmark refers to unavailable provider " + bm.Provider
	return nil
}

// advanceRestore drills one level deeper along m.restorePath, if any.
func (m *model) advanceRestore() tea.Cmd {
	if len(m.restorePath) == 0 {
		if m.restoreLabel != "" {
			m.status = "★ " + m.restoreLabel
			m.restoreLabel = ""
		}
		return nil
	}
	next := m.restorePath[0]
	for i, n := range m.visibleNodes {
		if (next.ID != "" && n.ID == next.ID) || (next.ID == "" && n.Name == next.Name) {
			m.table.SetCursor(i)
			m.restorePath = m.restorePath[1:]
			return m.drillDown()
		}
	}
	m.status = fmt.Sprintf("restore stopped at %q (not found)", next.Name)
	m.restorePath = nil
	m.restoreLabel = ""
	return nil
}

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
	coster, ok := m.active.(provider.Coster)
	if !ok {
		m.status = m.active.Name() + ": costs not supported yet"
		m.refreshTable()
		return nil
	}
	scope, ok := m.costScope()
	if !ok {
		m.status = m.costHint()
		m.refreshTable()
		return nil
	}
	if _, cached := m.costs[scope.ID]; cached {
		m.refreshTable()
		m.status = "cost column on (cached)"
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

func (m *model) costScope() (provider.Node, bool) {
	top := &m.stack[len(m.stack)-1]
	switch kindOf(top) {
	case provider.KindResourceGroup:
		if top.parent != nil && top.parent.Kind == provider.KindSubscription {
			return *top.parent, true
		}
	case provider.KindRegion:
		if top.parent != nil && top.parent.Kind == provider.KindAccount {
			return *top.parent, true
		}
	case provider.KindProject:
		return provider.Node{ID: "gcp", Kind: provider.KindCloud}, true
	}
	return provider.Node{}, false
}

func (m *model) costHint() string {
	switch m.active.Name() {
	case "azure":
		return "cost column on — drill into a subscription's resource groups"
	case "aws":
		return "cost column on — drill into the account's regions"
	case "gcp":
		return "cost column on — press c on the projects list"
	default:
		return "cost column on — not supported at this view"
	}
}

func (m *model) saveBookmark() {
	if len(m.stack) <= 1 || m.active == nil {
		m.status = "nothing to bookmark at this level"
		return
	}
	path := make([]config.Crumb, 0, len(m.stack))
	labelParts := make([]string, 0, len(m.stack))
	for i, f := range m.stack {
		if i == 0 {
			path = append(path, config.Crumb{Kind: string(provider.KindCloud), Name: m.active.Name()})
			labelParts = append(labelParts, m.active.Name())
			continue
		}
		if f.parent == nil {
			continue
		}
		path = append(path, config.Crumb{
			Kind: string(f.parent.Kind),
			ID:   f.parent.ID,
			Name: f.parent.Name,
		})
		labelParts = append(labelParts, f.parent.Name)
	}
	bm := config.Bookmark{
		Label:    strings.Join(labelParts, " / "),
		Provider: m.active.Name(),
		Path:     path,
		Created:  time.Now().UTC().Format(time.RFC3339),
	}
	m.cfg.AddBookmark(bm)
	if err := config.Save(m.cfg); err != nil {
		m.status = "bookmark save failed: " + err.Error()
		return
	}
	m.status = "★ bookmarked " + bm.Label
}

func (m *model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.searchMode = false
		m.search.Blur()
		m.filter = ""
		m.search.SetValue("")
		m.refreshTable()
		return m, nil
	case keyEnter:
		m.searchMode = false
		m.search.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.filter = m.search.Value()
	m.refreshTable()
	return m, cmd
}

func (m *model) drillDown() tea.Cmd {
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

func (m *model) resetView() {
	m.filter = ""
	m.search.SetValue("")
	m.searchMode = false
	m.search.Blur()
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
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return m.load(top.title, top.parent)
}

func (m *model) load(title string, parent *provider.Node) tea.Cmd {
	if m.active == nil {
		return nil
	}
	m.loading = true
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

func (m *model) loadDetail() tea.Cmd {
	if m.active == nil {
		return nil
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	cur := m.visibleNodes[c]
	if cur.Kind == provider.KindCloud || cur.Kind == provider.KindCloudDisabled {
		return nil
	}
	m.loading = true
	m.status = "loading " + cur.Name + "..."
	prov := m.active
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			data, err := prov.Details(ctx, cur)
			if err != nil {
				return errMsg{err}
			}
			return detailLoadedMsg{title: cur.Name, body: string(data)}
		},
	)
}

func (m *model) loadPIM() tea.Cmd {
	if m.active == nil {
		m.status = "enter a cloud first (↵ on a cloud row) before requesting PIM roles"
		return nil
	}
	if _, ok := m.active.(provider.PIMer); !ok {
		m.status = m.active.Name() + ": JIT elevation not supported yet (planned — use Azure for now)"
		return nil
	}
	m.loading = true
	m.status = "loading PIM eligible roles..."
	prov := m.active.(provider.PIMer)
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			roles, err := prov.ListEligibleRoles(ctx)
			if err != nil {
				return errMsg{err}
			}
			return pimLoadedMsg{roles: roles}
		},
	)
}

func (m *model) updatePIM(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pimActivate {
		return m.updatePIMActivate(msg)
	}
	switch msg.String() {
	case keyEsc, "q":
		m.pimMode = false
		m.status = ""
		return m, nil
	case "up", "k":
		if m.pimCursor > 0 {
			m.pimCursor--
		}
		return m, nil
	case "down", "j":
		if m.pimCursor < len(m.pimRoles)-1 {
			m.pimCursor++
		}
		return m, nil
	case "a", "enter":
		if len(m.pimRoles) == 0 {
			return m, nil
		}
		m.pimActivate = true
		m.pimInput.SetValue("")
		m.pimInput.Focus()
		return m, nil
	case "+":
		if m.pimDuration < 8 {
			m.pimDuration++
		}
		return m, nil
	case "-":
		if m.pimDuration > 1 {
			m.pimDuration--
		}
		return m, nil
	}
	return m, nil
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
		role := m.pimRoles[m.pimCursor]
		m.pimActivate = false
		m.pimInput.Blur()
		m.loading = true
		m.status = fmt.Sprintf("activating %s on %s for %dh...", role.RoleName, scopeDisplay(role), m.pimDuration)
		prov := m.active.(provider.PIMer)
		ctx := m.ctx
		dur := m.pimDuration
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				err := prov.ActivateRole(ctx, role, reason, dur)
				return pimActivatedMsg{role: role.RoleName + " on " + scopeDisplay(role), err: err}
			},
		)
	}
	var cmd tea.Cmd
	m.pimInput, cmd = m.pimInput.Update(msg)
	return m, cmd
}

func scopeDisplay(r provider.PIMRole) string {
	if r.ScopeName != "" {
		return r.ScopeName
	}
	return r.Scope
}

func (m *model) execShell() tea.Cmd {
	if m.active == nil {
		return nil
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	cur := m.visibleNodes[c]
	subID := contextSubID(cur)
	if subID == "" {
		m.status = "no subscription context at this level"
		return nil
	}
	rg := contextRG(cur)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	banner := fmt.Sprintf("cloudnav exec  sub=%s  rg=%s  —  exit to return\n", truncID(subID), rg)
	script := fmt.Sprintf("printf %%s %q; exec %q", banner, shell)
	shellCmd := exec.Command("sh", "-c", script)
	shellCmd.Env = append(os.Environ(),
		"CLOUDNAV_SUB="+subID,
		"CLOUDNAV_SUB_NAME="+cur.Name,
		"AZURE_SUBSCRIPTION_ID="+subID,
	)
	if rg != "" {
		shellCmd.Env = append(shellCmd.Env, "CLOUDNAV_RG="+rg)
	}
	return tea.ExecProcess(shellCmd, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err}
		}
		return nil
	})
}

func contextSubID(n provider.Node) string {
	if id := n.Meta["subscriptionId"]; id != "" {
		return id
	}
	if n.Kind == provider.KindSubscription {
		return n.ID
	}
	if n.Parent != nil {
		return contextSubID(*n.Parent)
	}
	return ""
}

func contextRG(n provider.Node) string {
	if n.Kind == provider.KindResourceGroup {
		return n.Name
	}
	if n.Parent != nil && n.Parent.Kind == provider.KindResourceGroup {
		return n.Parent.Name
	}
	return ""
}

func truncID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func (m *model) openPortal() {
	if m.active == nil {
		return
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return
	}
	cur := m.visibleNodes[c]
	url := m.active.PortalURL(cur)
	if url == "" {
		return
	}
	go openURL(url)
	m.status = "opened " + url
}

func openURL(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "linux":
		c = exec.Command("xdg-open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = c.Start()
}

func (m *model) refreshTable() {
	top := &m.stack[len(m.stack)-1]
	m.visibleNodes = m.applyView(top.nodes)
	m.mergeCosts(top)
	// Clear rows so SetColumns doesn't re-render stale-shaped rows and panic.
	m.table.SetRows(nil)
	m.table.SetColumns(m.columnsFor(top))
	m.table.SetRows(m.rowsFromNodes(top.title, m.visibleNodes))
	c := m.table.Cursor()
	switch {
	case len(m.visibleNodes) == 0:
		m.table.SetCursor(0)
	case c < 0:
		m.table.SetCursor(0)
	case c >= len(m.visibleNodes):
		m.table.SetCursor(len(m.visibleNodes) - 1)
	}
}

func (m *model) applyView(nodes []provider.Node) []provider.Node {
	out := make([]provider.Node, 0, len(nodes))
	if m.filter == "" {
		out = append(out, nodes...)
	} else {
		q := strings.ToLower(m.filter)
		for _, n := range nodes {
			if strings.Contains(strings.ToLower(n.Name), q) {
				out = append(out, n)
			}
		}
	}
	if isCloudLevel(out) {
		// Preserve the insertion order defined in newModel (active providers
		// first as listed; disabled ones fall at the end naturally).
		return out
	}
	switch m.sort {
	case sortState:
		sort.SliceStable(out, func(i, j int) bool { return out[i].State < out[j].State })
	case sortLocation:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Location < out[j].Location })
	default:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
	}
	return out
}

func isCloudLevel(nodes []provider.Node) bool {
	if len(nodes) == 0 {
		return false
	}
	k := nodes[0].Kind
	return k == provider.KindCloud || k == provider.KindCloudDisabled
}

func (m *model) columnsFor(f *frame) []table.Column {
	switch kindOf(f) {
	case provider.KindCloud, provider.KindCloudDisabled:
		return []table.Column{{Title: "CLOUD", Width: 40}}
	case provider.KindSubscription:
		return []table.Column{
			{Title: "NAME", Width: 44},
			{Title: "TENANT", Width: 24},
			{Title: "STATE", Width: 12},
			{Title: "ID", Width: 40},
		}
	case provider.KindProject:
		cols := []table.Column{
			{Title: "NAME", Width: 40},
			{Title: "PROJECT ID", Width: 28},
			{Title: "STATE", Width: 14},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 16})
		}
		return cols
	case provider.KindAccount:
		return []table.Column{
			{Title: "ACCOUNT", Width: 18},
			{Title: "ARN", Width: 60},
			{Title: "STATE", Width: 12},
		}
	case provider.KindRegion:
		cols := []table.Column{
			{Title: "REGION", Width: 22},
			{Title: "ENDPOINT", Width: 38},
			{Title: "STATE", Width: 12},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 16})
		}
		return cols
	case provider.KindResourceGroup:
		cols := []table.Column{
			{Title: "NAME", Width: 48},
			{Title: "LOCATION", Width: 18},
			{Title: "STATE", Width: 14},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 14})
		}
		return cols
	case provider.KindResource:
		return []table.Column{
			{Title: "NAME", Width: 48},
			{Title: "TYPE", Width: 36},
			{Title: "LOCATION", Width: 20},
		}
	default:
		return []table.Column{{Title: "NAME", Width: 80}}
	}
}

func kindOf(f *frame) provider.Kind {
	if len(f.nodes) > 0 {
		return f.nodes[0].Kind
	}
	if f.title == "clouds" {
		return provider.KindCloud
	}
	return ""
}

func (m *model) rowsFromNodes(_ string, nodes []provider.Node) []table.Row {
	rows := make([]table.Row, 0, len(nodes))
	for _, n := range nodes {
		switch n.Kind {
		case provider.KindCloud, provider.KindCloudDisabled:
			rows = append(rows, table.Row{n.Name})
		case provider.KindSubscription:
			tenant := n.Meta["tenantName"]
			if tenant == "" {
				tenant = shortID(n.Meta["tenantId"])
			}
			rows = append(rows, table.Row{n.Name, tenant, n.State, shorten(n.ID, 40)})
		case provider.KindProject:
			row := table.Row{n.Name, n.ID, n.State}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindAccount:
			rows = append(rows, table.Row{n.Name, shorten(n.Meta["arn"], 60), n.State})
		case provider.KindRegion:
			row := table.Row{n.Name, shorten(n.Meta["endpoint"], 38), n.State}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindResourceGroup:
			row := table.Row{n.Name, n.Location, n.State}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindResource:
			rows = append(rows, table.Row{n.Name, n.Meta["type"], n.Location})
		default:
			rows = append(rows, table.Row{n.Name})
		}
	}
	return rows
}

func (m *model) mergeCosts(f *frame) {
	if !m.showCost {
		return
	}
	var costs map[string]string
	switch kindOf(f) {
	case provider.KindResourceGroup:
		if f.parent != nil {
			costs = m.costs[f.parent.ID]
		}
	case provider.KindRegion:
		if f.parent != nil {
			costs = m.costs[f.parent.ID]
		}
		for i, n := range m.visibleNodes {
			if c, ok := costs[strings.ToLower(n.Name)]; ok {
				m.visibleNodes[i].Cost = c
			}
		}
		return
	case provider.KindProject:
		costs = m.costs["gcp"]
	}
	if costs == nil {
		return
	}
	for i, n := range m.visibleNodes {
		key := strings.ToLower(n.Name)
		if c, ok := costs[key]; ok {
			m.visibleNodes[i].Cost = c
		} else if c, ok := costs[strings.ToLower(n.ID)]; ok {
			m.visibleNodes[i].Cost = c
		}
	}
}

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func costOrDash(c string) string {
	if c == "" {
		return "—"
	}
	return c
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func firstErrLine(err error) string {
	s := err.Error()
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "ERROR: ")
		return line
	}
	return s
}

func (m *model) View() string {
	if m.showHelp {
		return m.helpView()
	}
	if m.pimMode {
		return m.pimView()
	}
	if m.paletteMode {
		return m.paletteView()
	}
	if m.detailMode {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.detailHeader(),
			m.detail.View(),
			m.detailFooter(),
		)
	}
	body := m.table.View()
	if len(m.visibleNodes) == 0 && !m.loading {
		body = m.emptyBody()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		body,
		m.footerView(),
	)
}

func (m *model) pimView() string {
	lines := []string{
		styles.Title.Render("PIM eligible roles") + "  " +
			styles.Help.Render(fmt.Sprintf("%d role(s)  duration %dh (use +/- to change)", len(m.pimRoles), m.pimDuration)),
		"",
	}
	if len(m.pimRoles) == 0 {
		lines = append(lines,
			styles.Help.Render("  no eligible PIM assignments for this user"),
			"",
			styles.Help.Render("  if you expect some, check:"),
			styles.Help.Render("    • PIM is enabled on the tenant"),
			styles.Help.Render("    • you have read on roleEligibilityScheduleInstances"),
		)
	}
	for i, r := range m.pimRoles {
		marker := "  "
		body := fmt.Sprintf("%s%2d. %-40s  on  %s", marker, i+1, r.RoleName, scopeDisplay(r))
		if i == m.pimCursor {
			body = styles.Selected.Render(fmt.Sprintf("> %2d. %-40s  on  %s", i+1, r.RoleName, scopeDisplay(r)))
		}
		lines = append(lines, body)
	}
	if m.pimActivate && len(m.pimRoles) > 0 {
		role := m.pimRoles[m.pimCursor]
		lines = append(lines,
			"",
			styles.Help.Render("activate: ")+role.RoleName+"  on  "+scopeDisplay(role)+fmt.Sprintf("  for %dh", m.pimDuration),
			m.pimInput.View(),
			styles.Help.Render("enter submit  esc cancel"),
		)
	} else {
		lines = append(lines,
			"",
			styles.Help.Render("↑↓ / jk move  a activate  +/- duration  esc close"),
		)
	}
	return styles.Box.Render(strings.Join(lines, "\n"))
}

func (m *model) paletteView() string {
	window := 15
	if m.height > 10 {
		window = m.height - 8
	}
	if window < 5 {
		window = 5
	}
	start := 0
	if len(m.paletteItems) > window {
		start = m.paletteIdx - window/2
		if start < 0 {
			start = 0
		}
		if start+window > len(m.paletteItems) {
			start = len(m.paletteItems) - window
		}
	}
	end := start + window
	if end > len(m.paletteItems) {
		end = len(m.paletteItems)
	}

	counter := fmt.Sprintf("%d items", len(m.paletteItems))
	if len(m.paletteItems) > window {
		counter = fmt.Sprintf("%d–%d of %d", start+1, end, len(m.paletteItems))
	}
	lines := []string{
		styles.Title.Render("palette") + "  " + styles.Help.Render(counter),
		"",
		m.paletteInput.View(),
		"",
	}
	if start > 0 {
		lines = append(lines, styles.Help.Render("  ↑ "+fmt.Sprintf("%d more above", start)))
	}
	for i := start; i < end; i++ {
		it := m.paletteItems[i]
		line := "  " + it.label
		if i == m.paletteIdx {
			line = styles.Selected.Render("> " + it.label)
		}
		lines = append(lines, line)
	}
	if end < len(m.paletteItems) {
		lines = append(lines, styles.Help.Render("  ↓ "+fmt.Sprintf("%d more below", len(m.paletteItems)-end)))
	}
	if len(m.paletteItems) == 0 {
		lines = append(lines, styles.Help.Render("  no matches"))
	}
	lines = append(lines,
		"",
		styles.Help.Render("↑↓ nav  ↵ select  esc close  (type to filter)"),
	)
	return styles.Box.Render(strings.Join(lines, "\n"))
}

func (m *model) emptyBody() string {
	msg := "  no items here"
	if m.filter != "" {
		msg = fmt.Sprintf("  no matches for %q  (esc to clear filter)", m.filter)
	}
	if len(m.stack) > 0 && m.err == nil && len(m.stack[len(m.stack)-1].nodes) == 0 {
		msg = "  empty — drill back with esc and try another path"
	}
	return styles.Help.Render("\n"+msg) + "\n"
}

func (m *model) detailHeader() string {
	title := styles.Title.Render("cloudnav") + "  " + styles.Crumb.Render("detail › "+m.detailTitle)
	right := styles.Help.Render(fmt.Sprintf("%d%%", int(m.detail.ScrollPercent()*100)))
	if m.width == 0 {
		return title + "   " + right
	}
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return title + strings.Repeat(" ", gap) + right
}

func (m *model) detailFooter() string {
	hints := strings.Join([]string{
		styles.Key.Render("↑↓") + " scroll",
		styles.Key.Render("esc") + " close",
		styles.Key.Render("q") + " close",
	}, "  ")
	return styles.StatusBar.Render(hints)
}

func (m *model) headerView() string {
	title := styles.Title.Render("cloudnav")
	crumbs := strings.Join(breadcrumbs(m.stack), styles.CrumbSep)
	left := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", styles.Crumb.Render(crumbs))
	rightBits := []string{}
	if len(m.stack) > 0 {
		total := len(m.stack[len(m.stack)-1].nodes)
		shown := len(m.visibleNodes)
		if m.filter != "" && shown != total {
			rightBits = append(rightBits, fmt.Sprintf("%d/%d", shown, total))
		} else {
			rightBits = append(rightBits, fmt.Sprintf("%d", total))
		}
	}
	if m.filter != "" {
		rightBits = append(rightBits, "filter: "+m.filter)
	}
	rightBits = append(rightBits, "sort: "+m.sort.String())
	rightBits = append(rightBits, currentProvider(m))
	right := styles.Help.Render(strings.Join(rightBits, "  "))
	if m.width == 0 {
		return left + "   " + right
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func breadcrumbs(stack []frame) []string {
	out := make([]string, 0, len(stack))
	for _, f := range stack {
		out = append(out, f.title)
	}
	return out
}

func currentProvider(m *model) string {
	if m.active == nil {
		return "—"
	}
	return m.active.Name()
}

func (m *model) footerView() string {
	if m.searchMode {
		return styles.StatusBar.Render(m.search.View())
	}
	if m.loading {
		return styles.StatusBar.Render(m.spinner.View() + " " + m.status)
	}
	if m.err != nil {
		msg := firstErrLine(m.err)
		budget := m.width - len("error: ") - 2
		if budget > 10 {
			msg = shorten(msg, budget)
		}
		return styles.StatusBar.Render(styles.Bad.Render("error: ") + msg)
	}
	hints := []string{
		styles.Key.Render("↵") + " open",
		styles.Key.Render("esc") + " back",
		styles.Key.Render("/") + " search",
		styles.Key.Render("s") + " sort",
		styles.Key.Render("o") + " portal",
		styles.Key.Render("r") + " refresh",
		styles.Key.Render("?") + " help",
		styles.Key.Render("q") + " quit",
	}
	line := strings.Join(hints, "  ")
	if m.status != "" {
		line += "   " + styles.Help.Render(m.status)
	}
	return styles.StatusBar.Render(line)
}

func (m *model) helpView() string {
	body := strings.Join([]string{
		styles.Title.Render("keybindings"),
		"",
		"↵ / l          drill down",
		"esc / h        back",
		"j k / ↑ ↓      move selection",
		"/              filter current view",
		":              palette — search any sub/project/account, switch cloud, jump to bookmark",
		"f              bookmark the current view (persisted)",
		"c              toggle cost column  (Azure RGs / AWS regions — MoM delta when available)",
		"s              cycle sort (name / state / location)",
		"o              open selected in cloud portal",
		"i              json detail",
		"p              PIM eligible roles (Azure) — j/k select, a activate, +/- duration",
		"x              exec provider CLI inside the selected scope",
		"r              refresh",
		"?              help",
		"q / ctrl+c     quit",
		"",
		styles.Help.Render("press any key to close"),
	}, "\n")
	return styles.Box.Render(body)
}
