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
	"sync"
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
	keyEsc           = "esc"
	keyEnter         = "enter"
	keyUp            = "up"
	keyDown          = "down"
	statusCostCached = "cost column on (cached)"
)

func Run() error {
	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type frame struct {
	title      string
	parent     *provider.Node
	nodes      []provider.Node
	aggregated bool // multi-RG resources view
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
	pimLoadedMsg struct{ roles []provider.PIMRole }

	advisorLoadedMsg struct {
		recs      []provider.Recommendation
		scope     string
		scopeName string
	}
	loginDoneMsg    struct{ cloud string }
	loginStatusMsg  struct{ status map[string]string }
	pimActivatedMsg struct {
		role      string
		roleID    string
		expiresAt string
		err       error
	}
	locksLoadedMsg struct {
		subID string
		locks map[string][]azure.Lock
	}
	lockChangedMsg struct {
		subID string
		msg   string
		err   error
	}
	deletedMsg struct {
		msg string
		err error
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
	ctx            context.Context
	providers      []provider.Provider
	active         provider.Provider
	stack          []frame
	visibleNodes   []provider.Node
	table          table.Model
	spinner        spinner.Model
	search         textinput.Model
	detail         viewport.Model
	detailTitle    string
	detailMode     bool
	searchMode     bool
	filter         string
	sort           sortMode
	loading        bool
	err            error
	status         string
	showHelp       bool
	paletteMode    bool
	paletteInput   textinput.Model
	paletteItems   []paletteItem
	paletteIdx     int
	cfg            *config.Config
	showCost       bool
	costs          map[string]map[string]string       // subID → lowercased rg name → cost
	tenantFilter   string                             // only show subs whose Meta[tenantName] == this (empty = all)
	locks          map[string]map[string][]azure.Lock // subID → rgName(lower) → locks
	selected       map[string]bool                    // node ID → selected
	restorePath    []config.Crumb                     // remaining crumbs to drill into during bookmark restore
	restoreLabel   string                             // label shown while restoring (for status)
	entities       map[string][]provider.Node         // provider name → top-level entities (subs/projects/accounts)
	pimMode        bool
	pimRoles       []provider.PIMRole
	pimCursor      int
	pimActivate    bool
	pimInput       textinput.Model
	pimFilter      string
	pimFilterOn    bool
	pimFilterIn    textinput.Model
	pimDuration    int
	pimSourceFilt  string // "" = all, pimSrc{Azure,Entra,Group}
	advisorMode    bool
	advisorRecs    []provider.Recommendation
	advisorScope   string
	advisorName    string
	advisorIdx     int
	loginStatus    map[string]string // providerName → human-readable auth state
	drilling       bool              // a drill-level load is in flight; block navigation
	categoryFilter string            // resource category on the resource list (compute / data / network / security / other)
	deleteMode     bool
	deleteTargets  []provider.Node
	deleteInput    textinput.Model
	width          int
	height         int
	keys           keys.Map
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
		BorderStyle(lipgloss.Border{}).
		Bold(true).
		Foreground(styles.Fg).
		Padding(0, 1)
	ts.Selected = ts.Selected.
		Background(styles.Purple).
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)
	ts.Cell = ts.Cell.Padding(0, 1)
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

	pimFilt := textinput.New()
	pimFilt.Prompt = "filter PIM: "
	pimFilt.Placeholder = "tenant, subscription, or role..."
	pimFilt.CharLimit = 120
	pimFilt.PromptStyle = lipgloss.NewStyle().Foreground(styles.Cyan).Bold(true)

	delIn := textinput.New()
	delIn.Prompt = "type DELETE to confirm: "
	delIn.Placeholder = "DELETE"
	delIn.CharLimit = 16
	delIn.PromptStyle = lipgloss.NewStyle().Foreground(styles.Err).Bold(true)

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle()

	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}

	m := &model{
		ctx:          context.Background(),
		providers:    buildProviders(cfg),
		spinner:      sp,
		search:       ti,
		paletteInput: pi,
		pimInput:     pimIn,
		pimFilterIn:  pimFilt,
		deleteInput:  delIn,
		pimDuration:  8,
		detail:       vp,
		cfg:          cfg,
		costs:        map[string]map[string]string{},
		entities:     map[string][]provider.Node{},
		locks:        map[string]map[string][]azure.Lock{},
		selected:     map[string]bool{},
		loginStatus:  map[string]string{},
		keys:         keys.Default(),
		table:        t,
		showCost:     true,
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

// buildProviders constructs the three cloud providers and threads user config
// into the ones that need it. Keeping the wiring in a helper keeps newModel
// from growing a big switch statement every time a provider adds a setting.
func buildProviders(cfg *config.Config) []provider.Provider {
	g := gcp.New()
	if cfg != nil && cfg.GCP.BillingTable != "" {
		g.SetBillingTable(cfg.GCP.BillingTable)
	}
	return []provider.Provider{azure.New(), g, aws.New()}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.checkLogins())
}

// checkLogins pings each provider's LoggedIn() concurrently and reports back
// via loginStatusMsg so the home cloud list can badge each row with the
// user's current auth state. Purely informational — drilling into a cloud
// still triggers Root() which surfaces fresh errors.
func (m *model) checkLogins() tea.Cmd {
	providers := m.providers
	ctx := m.ctx
	return func() tea.Msg {
		result := map[string]string{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, p := range providers {
			p := p
			wg.Add(1)
			go func() {
				defer wg.Done()
				status := loginStateFor(ctx, p)
				mu.Lock()
				result[p.Name()] = status
				mu.Unlock()
			}()
		}
		wg.Wait()
		return loginStatusMsg{status: result}
	}
}

// loginStateFor returns a short UI-ready status string: the CLI missing, the
// signed-in identity, or a "not logged in" hint.
func loginStateFor(ctx context.Context, p provider.Provider) string {
	if l, ok := p.(provider.Loginer); ok {
		bin, _ := l.LoginCommand()
		if _, err := exec.LookPath(bin); err != nil {
			return "CLI not installed"
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.LoggedIn(cctx); err != nil {
		return "not logged in"
	}
	return "logged in"
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if w := msg.Width; w > 0 {
			m.table.SetWidth(w)
			m.search.Width = w - 4
			m.detail.Width = w
		}
		// 2 header lines + 1 blank + 1 footer = 4 rows of chrome
		if h := msg.Height - 5; h > 0 {
			m.table.SetHeight(h)
			m.detail.Height = h
		}
		m.refreshTable()
		return m, nil

	case tea.KeyMsg:
		if m.deleteMode {
			return m.updateDeleteConfirm(msg)
		}
		if m.pimMode {
			return m.updatePIM(msg)
		}
		if m.advisorMode {
			return m.updateAdvisor(msg)
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
		// Lock navigation while the current frame is still loading and has
		// nothing on screen yet — otherwise the cursor moves behind a blank
		// view and any keystroke that fires a new command stacks on top of
		// the in-flight one. Quit and back still work so the user can bail.
		if m.isDrillLoading() {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Back):
				return m, m.goBack()
			}
			return m, nil
		}
		// 0-5 cycle the category filter on the resource list view. Cheap
		// shortcut to go from "all 500 things in this RG/project" to "just
		// compute" without typing in the / search.
		if m.atResourceLevel() {
			switch msg.String() {
			case "0":
				return m, m.setCategoryFilter("")
			case "1":
				return m, m.setCategoryFilter(catCompute)
			case "2":
				return m, m.setCategoryFilter(catData)
			case "3":
				return m, m.setCategoryFilter(catNetwork)
			case "4":
				return m, m.setCategoryFilter(catSecurity)
			case "5":
				return m, m.setCategoryFilter(catOther)
			}
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
		case key.Matches(msg, m.keys.Tenant):
			m.cycleTenant()
			return m, nil
		case key.Matches(msg, m.keys.Lock):
			return m, m.toggleLock()
		case key.Matches(msg, m.keys.Select):
			m.toggleSelection()
			return m, nil
		case key.Matches(msg, m.keys.SelectAll):
			m.selectAllVisible()
			return m, nil
		case key.Matches(msg, m.keys.ClearSel):
			m.selected = map[string]bool{}
			m.status = "selection cleared"
			m.refreshTable()
			return m, nil
		case key.Matches(msg, m.keys.Delete):
			m.promptDelete()
			return m, nil
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
		case key.Matches(msg, m.keys.Login):
			return m, m.loginCurrentCloud()
		case key.Matches(msg, m.keys.Advisor):
			return m, m.loadAdvisor()
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
		m.drilling = false
		m.err = nil
		m.stack = append(m.stack, msg.frame)
		m.refreshTable()
		m.table.SetCursor(0)
		m.status = fmt.Sprintf("%d items", len(msg.frame.nodes))
		if len(msg.frame.nodes) > 0 {
			if cap := msg.frame.nodes[0].Meta["partial"]; cap != "" {
				m.status = fmt.Sprintf("showing first %s — project has more; use / to filter", cap)
			}
		}
		cmds := []tea.Cmd{}
		if cmd := m.advanceRestore(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.maybeLoadLocks(msg.frame); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.maybeAutoLoadCost(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)

	case locksLoadedMsg:
		if m.locks == nil {
			m.locks = map[string]map[string][]azure.Lock{}
		}
		m.locks[msg.subID] = msg.locks
		m.refreshTable()
		return m, nil

	case lockChangedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.status = msg.msg
		delete(m.locks, msg.subID)
		return m, m.reloadLocksForActive()

	case deletedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.status = msg.msg
		m.selected = map[string]bool{}
		return m, m.reload()

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
		m.syncPIMDurationToPolicy()
		m.status = fmt.Sprintf("%d eligible role assignment(s)", len(msg.roles))
		return m, nil

	case loginDoneMsg:
		m.status = "✓ " + msg.cloud + " login complete — drill in to load"
		// Re-push home so the cloud list re-renders with updated login state.
		m.pushHome()
		return m, m.checkLogins()

	case loginStatusMsg:
		for k, v := range msg.status {
			m.loginStatus[k] = v
		}
		m.refreshTable()
		return m, nil

	case advisorLoadedMsg:
		m.loading = false
		m.err = nil
		m.advisorRecs = msg.recs
		m.advisorScope = msg.scope
		m.advisorName = msg.scopeName
		m.advisorIdx = 0
		m.advisorMode = true
		m.status = fmt.Sprintf("%d advisor recommendation(s) for %s", len(msg.recs), msg.scopeName)
		return m, nil

	case pimActivatedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.status = ""
			return m, nil
		}
		m.err = nil
		for i := range m.pimRoles {
			if m.pimRoles[i].ID == msg.roleID {
				m.pimRoles[i].Active = true
				m.pimRoles[i].ActiveUntil = msg.expiresAt
				break
			}
		}
		m.status = "✓ activation requested for " + msg.role + " — may take ~1 min to become effective"
		return m, nil

	case errMsg:
		m.loading = false
		m.drilling = false
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
	case keyUp:
		if m.paletteIdx > 0 {
			m.paletteIdx--
		}
		return m, nil
	case keyDown:
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
	case "USD", "":
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
		return provider.Node{ID: "gcp", Kind: provider.KindCloud}, true
	}
	return provider.Node{}, false
}

func (m *model) costHint() string {
	switch m.active.Name() {
	case pimSrcAzure:
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
	case keyUp, keyDown, "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.filter = m.search.Value()
	m.refreshTable()
	return m, cmd
}

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
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
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
			return nodesLoadedMsg{frame: frame{
				title:      fmt.Sprintf("%d resource group(s)", len(selected)),
				nodes:      combined,
				aggregated: true,
			}}
		},
	)
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
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return m.load(top.title, top.parent)
}

func (m *model) load(title string, parent *provider.Node) tea.Cmd {
	if m.active == nil {
		return nil
	}
	m.loading = true
	m.drilling = true
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

// loadAdvisor fetches cloud-native advisor recommendations for the scope
// under the cursor and opens the advisor overlay. Azure goes to ARM Advisor,
// GCP goes to Cloud Recommender. The provider.Advisor interface lets future
// clouds drop in without touching the TUI.
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
	case "gcp":
		projID, name := m.gcpAdvisorTarget()
		return projID, "projects/" + projID, name
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

// loginCurrentCloud runs the active cloud's CLI login interactively. Suspends
// the TUI via tea.ExecProcess so the browser redirect / device-code prompt
// the cloud CLI prints land in the user's terminal. On return the TUI
// refreshes so the cloud's nodes populate without a manual relaunch. When
// the CLI itself is missing we run the install plan first (single TUI
// suspension), then fall through to the login step.
func (m *model) loginCurrentCloud() tea.Cmd {
	prov := m.loginTargetProvider()
	if prov == nil {
		m.status = "move the cursor to a cloud row and press I to login"
		return nil
	}
	loginer, ok := prov.(provider.Loginer)
	if !ok {
		m.status = prov.Name() + ": login flow not implemented"
		return nil
	}
	bin, args := loginer.LoginCommand()
	providerName := prov.Name()
	if _, err := exec.LookPath(bin); err != nil {
		return m.installThenLogin(prov, loginer, bin, args)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	m.status = "launching " + bin + " " + strings.Join(args, " ") + "..."
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return errMsg{fmt.Errorf("%s login failed: %w", providerName, err)}
		}
		return loginDoneMsg{cloud: providerName}
	})
}

// installThenLogin chains install + login into a single sh -c so both run in
// one TUI suspension. Falls back to a clear error if no install recipe
// exists for the current OS.
func (m *model) installThenLogin(prov provider.Provider, loginer provider.Loginer, loginBin string, loginArgs []string) tea.Cmd {
	installer, ok := prov.(provider.Installer)
	if !ok {
		m.status = prov.Name() + ": " + loginer.InstallHint()
		return nil
	}
	steps, ok := installer.InstallPlan(runtime.GOOS)
	if !ok {
		m.status = fmt.Sprintf("no install recipe for %s on %s — %s", prov.Name(), runtime.GOOS, loginer.InstallHint())
		return nil
	}
	// Chain install steps then login into one shell so the TUI suspends
	// exactly once and the user sees the full output in order.
	parts := make([]string, 0, len(steps)+1)
	for _, s := range steps {
		parts = append(parts, shellQuote(s.Bin, s.Args))
	}
	parts = append(parts, shellQuote(loginBin, loginArgs))
	script := strings.Join(parts, " && ")
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = os.Environ()
	m.status = "installing " + prov.Name() + " CLI..."
	providerName := prov.Name()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return errMsg{fmt.Errorf("%s install/login failed: %w", providerName, err)}
		}
		return loginDoneMsg{cloud: providerName}
	})
}

// shellQuote produces a single shell-safe command string. Sufficient for the
// handful of argv's InstallPlan returns — none contain quotes or globs.
func shellQuote(bin string, args []string) string {
	parts := []string{bin}
	for _, a := range args {
		if strings.ContainsAny(a, " \t'\"") {
			parts = append(parts, "'"+strings.ReplaceAll(a, "'", "'\\''")+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// loginTargetProvider picks which provider the login key targets. When the
// user is on the home (cloud list) view, the cursor row chooses. Once drilled
// into a cloud, the active provider is used so the user can re-auth without
// going back.
func (m *model) loginTargetProvider() provider.Provider {
	if len(m.stack) > 0 {
		top := &m.stack[len(m.stack)-1]
		if kindOf(top) == provider.KindCloud || kindOf(top) == provider.KindCloudDisabled {
			c := m.table.Cursor()
			if c >= 0 && c < len(m.visibleNodes) {
				name := m.visibleNodes[c].Name
				for _, p := range m.providers {
					if p.Name() == name {
						return p
					}
				}
			}
		}
	}
	return m.active
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
	// Remember cursor across the SetRows(nil)/SetColumns/SetRows dance — the
	// intermediate SetRows(nil) can reset bubbles/table's cursor to -1, which
	// would snap the user back to the top on every refresh (e.g. space-select).
	prev := m.table.Cursor()
	top := &m.stack[len(m.stack)-1]
	m.visibleNodes = m.applyView(top.nodes)
	m.mergeCosts(top)
	cols := m.columnsFor(top)
	rows := m.rowsFromNodes(top.title, m.visibleNodes)
	normalized := make([]table.Row, len(rows))
	for i, r := range rows {
		nr := make(table.Row, len(cols))
		for j := range cols {
			if j < len(r) {
				nr[j] = r[j]
			}
		}
		normalized[i] = nr
	}
	m.table.SetRows(nil)
	m.table.SetColumns(cols)
	m.table.SetRows(normalized)
	switch {
	case len(m.visibleNodes) == 0:
		m.table.SetCursor(0)
	case prev < 0:
		m.table.SetCursor(0)
	case prev >= len(m.visibleNodes):
		m.table.SetCursor(len(m.visibleNodes) - 1)
	default:
		m.table.SetCursor(prev)
	}
}

func (m *model) applyView(nodes []provider.Node) []provider.Node {
	out := make([]provider.Node, 0, len(nodes))
	q := strings.ToLower(m.filter)
	tf := strings.ToLower(m.tenantFilter)
	cat := m.categoryFilter
	for _, n := range nodes {
		if tf != "" && strings.ToLower(n.Meta["tenantName"]) != tf {
			continue
		}
		if cat != "" && n.Kind == provider.KindResource && resourceCategory(n) != cat {
			continue
		}
		if q == "" ||
			strings.Contains(strings.ToLower(n.Name), q) ||
			strings.Contains(strings.ToLower(n.Meta["type"]), q) ||
			strings.Contains(strings.ToLower(n.State), q) ||
			strings.Contains(strings.ToLower(n.Meta["tenantName"]), q) {
			out = append(out, n)
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

// setCategoryFilter updates the active resource-category filter and triggers
// a table refresh. Returns nil so callers can do `return m, m.setCategoryFilter(...)`.
func (m *model) setCategoryFilter(cat string) tea.Cmd {
	m.categoryFilter = cat
	if cat == "" {
		m.status = "category: all"
	} else {
		m.status = "category: " + cat
	}
	m.refreshTable()
	return nil
}

// Resource category constants used by the category filter bar on the
// resource-list view. Kept short so the tab row stays readable.
const (
	catCompute  = "compute"
	catData     = "data"
	catNetwork  = "network"
	catSecurity = "security"
	catOther    = "other"
)

// resourceCategory sorts a Node into one of ~5 buckets based on its type
// string. The mapping is deliberately coarse — users want "show me compute"
// not a 30-way faceted search — and covers Azure / GCP / AWS with the same
// function so the TUI tab bar stays provider-agnostic.
func resourceCategory(n provider.Node) string {
	t := strings.ToLower(n.Meta["type"])
	switch {
	// Compute (VMs, serverless, containers, batch)
	case strings.Contains(t, "microsoft.compute/"),
		strings.Contains(t, "microsoft.containerservice"),
		strings.Contains(t, "microsoft.web/"),
		strings.Contains(t, "microsoft.containerinstance"),
		strings.Contains(t, "microsoft.containerregistry"),
		strings.Contains(t, "microsoft.batch"),
		strings.Contains(t, "microsoft.dataproc"),
		strings.Contains(t, "compute.googleapis.com"),
		strings.Contains(t, "container.googleapis.com"),
		strings.Contains(t, "run.googleapis.com"),
		strings.Contains(t, "cloudfunctions.googleapis.com"),
		strings.Contains(t, "workflows.googleapis.com"),
		strings.Contains(t, "artifactregistry.googleapis.com"),
		strings.HasPrefix(t, "ec2:"),
		strings.HasPrefix(t, "lambda:"),
		strings.HasPrefix(t, "ecs:"),
		strings.HasPrefix(t, "eks:"),
		strings.HasPrefix(t, "batch:"),
		strings.HasPrefix(t, "ecr:"):
		return catCompute

	// Data (relational, NoSQL, cache, analytics, object storage)
	case strings.Contains(t, "microsoft.sql"),
		strings.Contains(t, "microsoft.storage"),
		strings.Contains(t, "microsoft.documentdb"),
		strings.Contains(t, "microsoft.cache"),
		strings.Contains(t, "microsoft.dbforpostgresql"),
		strings.Contains(t, "microsoft.dbformysql"),
		strings.Contains(t, "microsoft.dbformariadb"),
		strings.Contains(t, "microsoft.synapse"),
		strings.Contains(t, "microsoft.datafactory"),
		strings.Contains(t, "sqladmin.googleapis.com"),
		strings.Contains(t, "spanner.googleapis.com"),
		strings.Contains(t, "bigtable"),
		strings.Contains(t, "redis.googleapis.com"),
		strings.Contains(t, "memcache.googleapis.com"),
		strings.Contains(t, "firestore.googleapis.com"),
		strings.Contains(t, "storage.googleapis.com"),
		strings.Contains(t, "bigquery.googleapis.com"),
		strings.Contains(t, "dataflow.googleapis.com"),
		strings.Contains(t, "dataproc.googleapis.com"),
		strings.HasPrefix(t, "s3:"),
		strings.HasPrefix(t, "rds:"),
		strings.HasPrefix(t, "dynamodb:"),
		strings.HasPrefix(t, "elasticache:"),
		strings.HasPrefix(t, "redshift:"),
		strings.HasPrefix(t, "glue:"):
		return catData

	// Network
	case strings.Contains(t, "microsoft.network"),
		strings.Contains(t, "microsoft.cdn"),
		strings.Contains(t, "dns.googleapis.com"),
		strings.Contains(t, "networkconnectivity.googleapis.com"),
		strings.HasPrefix(t, "elasticloadbalancing:"),
		strings.HasPrefix(t, "route53:"),
		strings.HasPrefix(t, "apigateway:"),
		strings.HasPrefix(t, "cloudfront:"),
		strings.HasPrefix(t, "vpc:"):
		return catNetwork

	// Security (IAM, secrets, KMS)
	case strings.Contains(t, "microsoft.keyvault"),
		strings.Contains(t, "microsoft.managedidentity"),
		strings.Contains(t, "microsoft.security"),
		strings.Contains(t, "iam.googleapis.com"),
		strings.Contains(t, "secretmanager.googleapis.com"),
		strings.Contains(t, "cloudkms.googleapis.com"),
		strings.HasPrefix(t, "iam:"),
		strings.HasPrefix(t, "kms:"),
		strings.HasPrefix(t, "secretsmanager:"),
		strings.HasPrefix(t, "acm:"):
		return catSecurity
	}
	return catOther
}

// categoryCounts aggregates visibleNodes by category for the tab bar header.
func (m *model) categoryCounts(nodes []provider.Node) map[string]int {
	c := map[string]int{}
	for _, n := range nodes {
		if n.Kind != provider.KindResource {
			continue
		}
		c[resourceCategory(n)]++
	}
	return c
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
		return []table.Column{
			{Title: "CLOUD", Width: 20},
			{Title: "STATUS", Width: 40},
			{Title: "HINT", Width: 60},
		}
	case provider.KindSubscription:
		cols := []table.Column{
			{Title: "NAME", Width: 40},
			{Title: "TENANT", Width: 22},
			{Title: "STATE", Width: 10},
			{Title: "ID", Width: 38},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 22})
		}
		return cols
	case provider.KindProject:
		cols := []table.Column{
			{Title: "NAME", Width: 36},
			{Title: "PROJECT ID", Width: 28},
			{Title: "STATE", Width: 12},
			{Title: "CREATED", Width: 12},
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
			{Title: " ", Width: 3},
			{Title: "NAME", Width: 42},
			{Title: "LOCATION", Width: 16},
			{Title: "STATE", Width: 12},
			{Title: "LOCK", Width: 20},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 20})
		}
		return cols
	case provider.KindResource:
		cols := []table.Column{
			{Title: " ", Width: 4},
			{Title: "NAME", Width: 36},
			{Title: "TYPE", Width: 28},
			{Title: "LOCATION", Width: 12},
			{Title: "CREATED", Width: 12},
		}
		if f.aggregated {
			cols = append(cols, table.Column{Title: "RESOURCE GROUP", Width: 32})
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 20})
		}
		return cols
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
			status, hint := m.cloudRowStatus(n.Name)
			rows = append(rows, table.Row{n.Name, status, hint})
		case provider.KindSubscription:
			tenant := n.Meta["tenantName"]
			if tenant == "" {
				tenant = shortID(n.Meta["tenantId"])
			}
			row := table.Row{n.Name, tenant, n.State, shorten(n.ID, 38)}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindProject:
			row := table.Row{n.Name, n.ID, n.State, shortDate(n.Meta["createdTime"])}
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
			lock := lockBadgePlain(m.rgLockLevel(n.Name))
			row := table.Row{selectionMark(m.selected[n.ID]), n.Name, n.Location, n.State, lock}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindResource:
			row := table.Row{selectionMark(m.selected[n.ID]), n.Name, n.Meta["type"], n.Location, shortDate(n.Meta["createdTime"])}
			if len(m.stack) > 0 && m.stack[len(m.stack)-1].aggregated {
				row = append(row, n.Meta["originRG"])
			}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
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

func shortID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

const (
	emDash          = "—"
	pimSrcAzure     = "azure"
	pimSrcEntra     = "entra"
	pimSrcGroup     = "group"
	pimSrcGCP       = "gcp-pam"
	cliNotInstalled = "✗ CLI not installed"
)

func costOrDash(c string) string {
	if c == "" {
		return emDash
	}
	return c
}

// shortDate renders an ISO-8601 timestamp as "2026-01-15" for the audit
// column. Empty or unparseable input falls back to an em-dash.
func shortDate(iso string) string {
	if iso == "" {
		return emDash
	}
	if t, err := time.Parse(time.RFC3339, iso); err == nil {
		return t.Format("2006-01-02")
	}
	if len(iso) >= 10 {
		return iso[:10]
	}
	return emDash
}

func selectionMark(selected bool) string {
	if selected {
		return "[x]"
	}
	return "[ ]"
}

const (
	lockCanNotDelete = "CanNotDelete"
	lockReadOnly     = "ReadOnly"
)

func lockBadgePlain(level string) string {
	switch level {
	case lockCanNotDelete:
		return "🔒 CanNotDelete"
	case lockReadOnly:
		return "🔒 ReadOnly"
	default:
		return emDash
	}
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
	if m.deleteMode {
		return m.deleteConfirmView()
	}
	if m.showHelp {
		return m.helpView()
	}
	if m.pimMode {
		return m.pimView()
	}
	if m.advisorMode {
		return m.advisorView()
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
	if m.isDrillLoading() {
		body = m.drillLoadingBody()
	} else if len(m.visibleNodes) == 0 && !m.loading && m.categoryFilter == "" {
		body = m.emptyBody()
	}
	chunks := []string{m.headerView()}
	if bar := m.categoryBar(); bar != "" {
		chunks = append(chunks, bar)
	}
	chunks = append(chunks, body, m.footerView())
	return lipgloss.JoinVertical(lipgloss.Left, chunks...)
}

// categoryBar renders the resource-category filter tabs. Only shown on the
// resource-list view (KindResource frames) where categories are meaningful.
// Counts reflect the parent frame's nodes (before filtering) so the numbers
// don't collapse to 0 when you're already filtering.
func (m *model) categoryBar() string {
	if len(m.stack) == 0 {
		return ""
	}
	top := &m.stack[len(m.stack)-1]
	if kindOf(top) != provider.KindResource {
		return ""
	}
	counts := m.categoryCounts(top.nodes)
	tab := func(key, label, cat string, n int) string {
		text := fmt.Sprintf("%s %s (%d)", key, label, n)
		if m.categoryFilter == cat {
			return styles.Selected.Render(" " + text + " ")
		}
		return styles.Help.Render(" " + text + " ")
	}
	tabs := strings.Join([]string{
		tab("0", "all", "", len(top.nodes)),
		tab("1", "compute", catCompute, counts[catCompute]),
		tab("2", "data", catData, counts[catData]),
		tab("3", "network", catNetwork, counts[catNetwork]),
		tab("4", "security", catSecurity, counts[catSecurity]),
		tab("5", "other", catOther, counts[catOther]),
	}, "")
	return " " + tabs
}

// drillLoadingBody is the big in-your-face loading panel shown while the
// initial node list is in flight. It replaces the table so there's nothing
// to accidentally navigate, and spells out exactly what cloudnav is waiting
// on plus the fact that input is disabled until it lands.
func (m *model) drillLoadingBody() string {
	title := styles.Title.Render("⏳ loading")
	detail := m.status
	if detail == "" {
		detail = "waiting for cloud response..."
	}
	lines := []string{
		"",
		"  " + title + "  " + m.spinner.View(),
		"",
		"  " + detail,
		"",
		styles.Help.Render("  input is disabled until this finishes — press esc to go back, q to quit"),
	}
	return strings.Join(lines, "\n")
}

func (m *model) updateAdvisor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc, "q", "A":
		m.advisorMode = false
		m.status = ""
		return m, nil
	case keyUp, "k":
		if m.advisorIdx > 0 {
			m.advisorIdx--
		}
		return m, nil
	case keyDown, "j":
		if m.advisorIdx < len(m.advisorRecs)-1 {
			m.advisorIdx++
		}
		return m, nil
	case "o":
		if m.advisorIdx >= 0 && m.advisorIdx < len(m.advisorRecs) {
			go openURL("https://portal.azure.com/#blade/Microsoft_Azure_Expert/AdvisorMenuBlade/overview")
			m.status = "opened Advisor in portal"
		}
		return m, nil
	}
	return m, nil
}

func (m *model) advisorView() string {
	header := styles.Title.Render("Azure Advisor") + "  " +
		styles.Help.Render(fmt.Sprintf("%d recommendation(s) for %s", len(m.advisorRecs), m.advisorName))
	if len(m.advisorRecs) == 0 {
		return styles.Box.Render(strings.Join([]string{
			header,
			"",
			"No recommendations at this scope.",
			"",
			styles.Help.Render("Advisor generates cost / security / reliability / performance tips."),
			styles.Help.Render("Drill further and press A again, or check the full Advisor in the portal."),
			"",
			styles.Help.Render("esc close   o open Advisor in portal"),
		}, "\n"))
	}

	lines := []string{header, ""}
	// Render the list on top, full detail for the cursor row below.
	max := len(m.advisorRecs)
	if max > 14 {
		max = 14
	}
	start := 0
	if m.advisorIdx >= max {
		start = m.advisorIdx - max + 1
	}
	for i := start; i < start+max && i < len(m.advisorRecs); i++ {
		r := m.advisorRecs[i]
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

	if m.advisorIdx >= 0 && m.advisorIdx < len(m.advisorRecs) {
		r := m.advisorRecs[m.advisorIdx]
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
	lines = append(lines, "", styles.Help.Render("↑↓/jk move   o portal   esc/A close"))
	return styles.Box.Render(strings.Join(lines, "\n"))
}

// cloudRowStatus returns (status, hint) for the cloud-list view. status says
// what the background LoggedIn probe found; hint tells a brand-new user what
// to do — ideally press I inside the TUI, or run the command from a shell.
func (m *model) cloudRowStatus(name string) (string, string) {
	st := m.loginStatus[name]
	switch st {
	case "":
		return "checking...", ""
	case "logged in":
		return "✓ logged in", "press ↵ to drill"
	case "CLI not installed":
		if p := m.providerByName(name); p != nil {
			if inst, ok := p.(provider.Installer); ok {
				if _, can := inst.InstallPlan(runtime.GOOS); can {
					return cliNotInstalled, "press I to auto-install + login"
				}
			}
			if l, ok := p.(provider.Loginer); ok {
				return cliNotInstalled, l.InstallHint()
			}
		}
		return cliNotInstalled, ""
	case "not logged in":
		if p := m.providerByName(name); p != nil {
			if l, ok := p.(provider.Loginer); ok {
				bin, args := l.LoginCommand()
				return "✗ not logged in", "press I to run '" + bin + " " + strings.Join(args, " ") + "'"
			}
		}
		return "✗ not logged in", "press I to login"
	default:
		return st, ""
	}
}

func (m *model) providerByName(name string) provider.Provider {
	for _, p := range m.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// pimSourceBadge renders a short, color-tagged label for the PIM surface.
func pimSourceBadge(src string) string {
	switch src {
	case pimSrcEntra:
		return styles.AccentS.Render("entra")
	case pimSrcGroup:
		return styles.WarnS.Render("group")
	case pimSrcGCP:
		return styles.Good.Render("gcp-pam")
	case pimSrcAzure, "":
		return styles.Help.Render(pimSrcAzure)
	default:
		return styles.Help.Render(src)
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

// padRight pads s with spaces so the *visible* width equals n. Uses
// lipgloss.Width so ANSI-styled strings measure correctly.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// shortTail returns the last n characters so long resource IDs keep the
// meaningful segment (resource name) rather than the subscription prefix.
func shortTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
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
		src := padRight(pimSourceBadge(r.Source), 8)
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
	return styles.Box.Render(strings.Join(lines, "\n"))
}

func (m *model) paletteView() string {
	window := 10
	if m.height > 14 {
		window = m.height - 12
	}
	if window < 4 {
		window = 4
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
	if m.filter != "" || m.tenantFilter != "" {
		parts := []string{}
		if m.filter != "" {
			parts = append(parts, fmt.Sprintf("filter %q", m.filter))
		}
		if m.tenantFilter != "" {
			parts = append(parts, fmt.Sprintf("tenant %q", m.tenantFilter))
		}
		msg = "  no matches for " + strings.Join(parts, " + ") + "  (esc to clear)"
	} else if len(m.stack) > 0 && m.err == nil && len(m.stack[len(m.stack)-1].nodes) == 0 {
		top := &m.stack[len(m.stack)-1]
		switch kindOf(top) {
		case provider.KindResource:
			if top.parent != nil {
				msg = fmt.Sprintf("  no resources inside %q — the resource group is empty", top.parent.Name)
			} else {
				msg = "  no resources found"
			}
		case provider.KindResourceGroup:
			msg = "  no resource groups in this subscription"
		case provider.KindRegion:
			msg = "  no active regions"
		case provider.KindSubscription:
			msg = "  no subscriptions visible — check `az login` or try a different tenant"
		case provider.KindProject:
			msg = "  no projects visible — check `gcloud auth login` and your org access"
		case provider.KindAccount:
			msg = "  no AWS account visible — check `aws configure` or SSO session"
		default:
			msg = "  empty at this level — drill back with esc"
		}
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
	path := []string{styles.Title.Render("cloudnav")}
	path = append(path, breadcrumbs(m.stack)...)
	crumb := strings.Join(path, styles.CrumbSep)
	right := styles.Help.Render("^_^")
	if m.width == 0 {
		return crumb + "   " + right + "\n" + m.keybar() + "\n"
	}
	gap := m.width - lipgloss.Width(crumb) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	top := crumb + strings.Repeat(" ", gap) + right
	return top + "\n" + m.keybar() + "\n"
}

func (m *model) keybar() string {
	type pair struct{ key, action string }
	pairs := []pair{
		{"↵", "drill"},
		{"/", "search"},
		{":", "palette"},
		{"f", "flag"},
		{"p", "PIM"},
		{"i", "info"},
		{"o", "portal"},
		{"c", "costs"},
		{"s", "sort " + m.sort.String()},
	}
	if _, ok := m.active.(provider.Advisor); ok && m.active != nil {
		pairs = append(pairs, pair{"A", "advisor"})
	}
	if m.atCloudLevel() {
		pairs = append(pairs, pair{"I", "login"})
	}
	if m.atSubscriptionLevel() {
		label := "tenant: all"
		if m.tenantFilter != "" {
			label = "tenant: " + m.tenantFilter
		}
		pairs = append(pairs, pair{"t", label})
	}
	if m.atRGLevel() {
		pairs = append(pairs,
			pair{"L", "lock"},
			pair{"␣", "select"},
		)
		if n := len(m.selected); n > 0 {
			pairs = append(pairs, pair{"D", fmt.Sprintf("delete %d", n)})
		}
	}
	if m.atResourceLevel() {
		pairs = append(pairs, pair{"␣", "select"})
	}
	pairs = append(pairs,
		pair{"r", "refresh"},
		pair{"esc", "back"},
		pair{"q", "quit"},
	)
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, styles.Key.Render("<"+p.key+">")+" "+styles.Help.Render(p.action))
	}
	return "  " + strings.Join(parts, "  ")
}

func (m *model) atSubscriptionLevel() bool {
	if len(m.stack) == 0 {
		return false
	}
	top := &m.stack[len(m.stack)-1]
	return kindOf(top) == provider.KindSubscription
}

// isDrillLoading reports whether a drill-level fetch is in flight — the user
// hit Enter on a cloud / sub / RG and we're waiting for Root() or Children()
// to return. Background fetches (cost streaming, policy lookups, lock
// probes) leave this false so the user can keep navigating their rows.
func (m *model) isDrillLoading() bool {
	return m.drilling
}

func (m *model) atCloudLevel() bool {
	if len(m.stack) == 0 {
		return false
	}
	top := &m.stack[len(m.stack)-1]
	return top.title == "clouds"
}

func (m *model) atRGLevel() bool {
	if len(m.stack) == 0 {
		return false
	}
	top := &m.stack[len(m.stack)-1]
	return kindOf(top) == provider.KindResourceGroup
}

func (m *model) atResourceLevel() bool {
	if len(m.stack) == 0 {
		return false
	}
	top := &m.stack[len(m.stack)-1]
	return kindOf(top) == provider.KindResource
}

func (m *model) currentSubID() string {
	if !m.atRGLevel() {
		return ""
	}
	top := &m.stack[len(m.stack)-1]
	if top.parent == nil {
		return ""
	}
	return top.parent.ID
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

func (m *model) maybeLoadLocks(f frame) tea.Cmd {
	if len(f.nodes) == 0 || f.nodes[0].Kind != provider.KindResourceGroup || f.parent == nil {
		return nil
	}
	az, ok := m.active.(*azure.Azure)
	if !ok {
		return nil
	}
	subID := f.parent.ID
	if _, cached := m.locks[subID]; cached {
		return nil
	}
	// mark as in-flight so the same drill doesn't fire twice
	m.locks[subID] = map[string][]azure.Lock{}
	ctx := m.ctx
	return func() tea.Msg {
		locks, err := az.ResourceGroupLocks(ctx, subID)
		if err != nil {
			return locksLoadedMsg{subID: subID, locks: map[string][]azure.Lock{}}
		}
		return locksLoadedMsg{subID: subID, locks: locks}
	}
}

func (m *model) reloadLocksForActive() tea.Cmd {
	subID := m.currentSubID()
	if subID == "" {
		return nil
	}
	az, ok := m.active.(*azure.Azure)
	if !ok {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		locks, err := az.ResourceGroupLocks(ctx, subID)
		if err != nil {
			return locksLoadedMsg{subID: subID, locks: map[string][]azure.Lock{}}
		}
		return locksLoadedMsg{subID: subID, locks: locks}
	}
}

func (m *model) rgLockLevel(rgName string) string {
	subID := m.currentSubID()
	if subID == "" {
		return ""
	}
	locks := m.locks[subID]
	if locks == nil {
		return ""
	}
	list := locks[strings.ToLower(rgName)]
	if len(list) == 0 {
		return ""
	}
	for _, lk := range list {
		if strings.EqualFold(lk.Level, lockReadOnly) {
			return lockReadOnly
		}
	}
	return lockCanNotDelete
}

func (m *model) toggleLock() tea.Cmd {
	if !m.atRGLevel() {
		m.status = "L works on the resource-groups view (Azure)"
		return nil
	}
	az, ok := m.active.(*azure.Azure)
	if !ok {
		m.status = "lock management is Azure-only"
		return nil
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return nil
	}
	rg := m.visibleNodes[c]
	subID := m.currentSubID()
	existing := m.locks[subID][strings.ToLower(rg.Name)]
	ctx := m.ctx
	if len(existing) > 0 {
		lk := existing[0]
		m.loading = true
		m.status = fmt.Sprintf("removing lock %q on %s...", lk.Name, rg.Name)
		return tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				err := az.DeleteRGLock(ctx, subID, rg.Name, lk.Name)
				return lockChangedMsg{
					subID: subID,
					msg:   fmt.Sprintf("removed lock %q from %s", lk.Name, rg.Name),
					err:   err,
				}
			},
		)
	}
	m.loading = true
	m.status = fmt.Sprintf("adding CanNotDelete lock on %s...", rg.Name)
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			err := az.CreateRGLock(ctx, subID, rg.Name, "cloudnav-protect", "CanNotDelete")
			return lockChangedMsg{
				subID: subID,
				msg:   fmt.Sprintf("added CanNotDelete lock on %s", rg.Name),
				err:   err,
			}
		},
	)
}

func (m *model) toggleSelection() {
	if len(m.visibleNodes) == 0 {
		return
	}
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visibleNodes) {
		return
	}
	id := m.visibleNodes[c].ID
	if m.selected[id] {
		delete(m.selected, id)
	} else {
		m.selected[id] = true
	}
	m.status = fmt.Sprintf("%d selected", len(m.selected))
	m.refreshTable()
}

func (m *model) selectAllVisible() {
	for _, n := range m.visibleNodes {
		m.selected[n.ID] = true
	}
	m.status = fmt.Sprintf("selected all %d visible", len(m.visibleNodes))
	m.refreshTable()
}

// promptDelete gates destructive RG deletion behind an explicit confirmation
// overlay. Validation up front (scope, provider, non-empty selection, no
// locks) short-circuits with a status hint; only a clean selection opens the
// confirmation modal. The actual azure call doesn't fire until the user types
// DELETE in executeDelete.
func (m *model) promptDelete() {
	if !m.atRGLevel() {
		m.status = "D works on the resource-groups view"
		return
	}
	if _, ok := m.active.(*azure.Azure); !ok {
		m.status = "delete is Azure-only"
		return
	}
	targets := []provider.Node{}
	for _, n := range m.visibleNodes {
		if m.selected[n.ID] {
			targets = append(targets, n)
		}
	}
	if len(targets) == 0 {
		m.status = "nothing selected — use space to select rows, [ to select all, D to delete"
		return
	}
	for _, t := range targets {
		if lv := m.rgLockLevel(t.Name); lv != "" {
			m.status = fmt.Sprintf("refused — %s has a %s lock; press L to remove it first", t.Name, lv)
			return
		}
	}
	m.deleteMode = true
	m.deleteTargets = targets
	m.deleteInput.SetValue("")
	m.deleteInput.Focus()
	m.status = ""
}

// executeDelete fires the actual async deletion after the user has typed the
// confirmation word. Runs one request per target and reports a single summary
// message — Azure handles the multi-hour async teardown.
func (m *model) executeDelete() tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok || len(m.deleteTargets) == 0 {
		m.deleteMode = false
		return nil
	}
	targets := m.deleteTargets
	subID := m.currentSubID()
	ctx := m.ctx
	m.deleteMode = false
	m.deleteInput.Blur()
	m.deleteTargets = nil
	m.selected = map[string]bool{}
	m.loading = true
	m.status = fmt.Sprintf("deleting %d resource group(s) asynchronously...", len(targets))
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			fails := 0
			for _, t := range targets {
				if err := az.DeleteResourceGroup(ctx, subID, t.Name); err != nil {
					fails++
				}
			}
			if fails > 0 {
				return deletedMsg{
					msg: fmt.Sprintf("%d of %d deletions failed", fails, len(targets)),
					err: fmt.Errorf("%d failures", fails),
				}
			}
			return deletedMsg{msg: fmt.Sprintf("requested deletion of %d RG(s) — Azure is processing", len(targets))}
		},
	)
}

// updateDeleteConfirm handles keys inside the delete confirmation overlay.
// Enter with "DELETE" typed fires; anything else (including esc or Enter with
// a wrong word) cancels. Matching is case-insensitive for user comfort but
// an empty input never proceeds — that's the safety floor.
func (m *model) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.deleteMode = false
		m.deleteTargets = nil
		m.deleteInput.Blur()
		m.status = "deletion cancelled"
		return m, nil
	case keyEnter:
		if strings.EqualFold(strings.TrimSpace(m.deleteInput.Value()), "DELETE") {
			return m, m.executeDelete()
		}
		m.deleteMode = false
		m.deleteTargets = nil
		m.deleteInput.Blur()
		m.status = "deletion cancelled — confirmation word did not match 'DELETE'"
		return m, nil
	}
	var cmd tea.Cmd
	m.deleteInput, cmd = m.deleteInput.Update(msg)
	return m, cmd
}

// deleteConfirmView renders the destructive-action modal. The disclaimer is
// blunt on purpose: the user is about to tell Azure to tear down resource
// groups that may hold production data, and cloudnav does not have the state
// or authority to undo that. Making the wording visible makes it harder to
// wave away.
func (m *model) deleteConfirmView() string {
	lines := []string{
		styles.Bad.Render("⚠  DELETE RESOURCE GROUPS"),
		"",
		styles.Header.Render("This will permanently delete:"),
	}
	for i, t := range m.deleteTargets {
		line := fmt.Sprintf("  %2d. %s   %s", i+1, t.Name, styles.Help.Render(t.Location))
		lines = append(lines, line)
	}
	lines = append(lines,
		"",
		styles.Bad.Render("Everything inside each resource group — VMs, databases, storage,"),
		styles.Bad.Render("keys, backups — goes with it. Azure async-tears it down and the"),
		styles.Bad.Render("operation cannot be undone once the request is accepted."),
		"",
		styles.Help.Render("cloudnav forwards the request to the Azure CLI. You are responsible"),
		styles.Help.Render("for the consequences of this action. Recovery is a support ticket,"),
		styles.Help.Render("and in most cases there is no recovery at all."),
		"",
		m.deleteInput.View(),
		"",
		styles.Help.Render("enter  proceed     esc  cancel"),
	)
	return styles.Box.Render(strings.Join(lines, "\n"))
}

func (m *model) cycleTenant() {
	if !m.atSubscriptionLevel() {
		m.status = "tenant filter applies to the subscriptions view"
		return
	}
	top := m.stack[len(m.stack)-1]
	seen := map[string]bool{}
	tenants := []string{}
	for _, n := range top.nodes {
		t := n.Meta["tenantName"]
		if t == "" {
			t = n.Meta["tenantId"]
		}
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)
	idx := -1
	for i, t := range tenants {
		if t == m.tenantFilter {
			idx = i
			break
		}
	}
	next := ""
	if idx+1 < len(tenants) {
		next = tenants[idx+1]
	}
	m.tenantFilter = next
	if next == "" {
		m.status = "tenant filter: all"
	} else {
		m.status = "tenant filter: " + next
	}
	m.refreshTable()
}

func breadcrumbs(stack []frame) []string {
	out := make([]string, 0, len(stack))
	for _, f := range stack {
		out = append(out, f.title)
	}
	return out
}

func (m *model) footerView() string {
	if m.searchMode {
		return " " + m.search.View()
	}
	// Filter context (tenant / search) always wins over the loading spinner
	// so the user can see what filter is active even while costs stream in.
	if filt := m.filterFooter(); filt != "" {
		if m.loading {
			return " " + m.loadingFooter(filt)
		}
		return " " + styles.Help.Render(filt)
	}
	if m.loading {
		return " " + m.loadingFooter(m.status)
	}
	if m.err != nil {
		msg := firstErrLine(m.err)
		budget := m.width - len("error: ") - 2
		if budget > 10 {
			msg = shorten(msg, budget)
		}
		return " " + styles.Bad.Render("error: ") + msg
	}
	right := ""
	total := 0
	if len(m.stack) > 0 {
		total = len(m.stack[len(m.stack)-1].nodes)
	}
	switch {
	case m.status != "":
		right = m.status
	case total > 0:
		right = fmt.Sprintf("%d items", total)
	}
	return " " + styles.Help.Render(right)
}

// loadingFooter renders the active-spinner footer line with the status in
// cyan + bold so it reads as "something is happening" instead of melting into
// the dim filter text.
func (m *model) loadingFooter(text string) string {
	return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(styles.Cyan).Bold(true).Render(text)
}

// filterFooter renders the tenant / search filter strip with an N/total
// counter. Returns "" when no filter is active.
func (m *model) filterFooter() string {
	total := 0
	if len(m.stack) > 0 {
		total = len(m.stack[len(m.stack)-1].nodes)
	}
	shown := len(m.visibleNodes)
	switch {
	case m.filter != "" && m.tenantFilter != "":
		return fmt.Sprintf("tenant: %s  filter: %s  %d/%d", m.tenantFilter, m.filter, shown, total)
	case m.filter != "":
		return fmt.Sprintf("filter: %s  %d/%d", m.filter, shown, total)
	case m.tenantFilter != "":
		return fmt.Sprintf("tenant: %s  %d/%d", m.tenantFilter, shown, total)
	}
	return ""
}

func (m *model) helpView() string {
	body := strings.Join([]string{
		styles.Title.Render("cloudnav keybindings"),
		styles.Header.Render("Nav") + "    ↵/l drill   esc/h back   jk move   / filter   : palette   f flag   r refresh",
		styles.Header.Render("View") + "   i info   o portal   c costs   s sort   t tenant",
		styles.Header.Render("Auth") + "   I login (runs az/gcloud/aws login inside the TUI)",
		styles.Header.Render("Select") + " ␣ toggle   [ select-all   ] clear   D delete   L lock",
		styles.Header.Render("Filter") + " 0-5 on resource views — 0 all / 1 compute / 2 data / 3 network / 4 security / 5 other",
		styles.Header.Render("Ops") + "    A advisor (Azure / GCP) — cost / security / reliability / perf / ops",
		styles.Header.Render("PIM") + "    p open — Azure / Entra / Groups / GCP PAM   0/1/2/3/4 filter source",
		"         / filter   a activate   +/- duration   j/k move",
		styles.Header.Render("Misc") + "   x exec   ? help   q quit",
		"",
		styles.Help.Render("press any key to close"),
	}, "\n")
	return styles.Box.Render(body)
}
