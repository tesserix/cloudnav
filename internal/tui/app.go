// Package tui hosts the Bubbletea application. It is deliberately cloud-agnostic:
// every cloud concept arrives through a provider.Provider and is rendered via
// generic table rows. Adding a new cloud means implementing the provider
// interface — not editing this package.
package tui

import (
	"context"
	"fmt"
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

	"github.com/tesserix/cloudnav/internal/cache"
	"github.com/tesserix/cloudnav/internal/config"
	"github.com/tesserix/cloudnav/internal/currency"
	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
	"github.com/tesserix/cloudnav/internal/tui/components"
	"github.com/tesserix/cloudnav/internal/tui/keys"
	"github.com/tesserix/cloudnav/internal/tui/styles"
	"github.com/tesserix/cloudnav/internal/updatecheck"
	"github.com/tesserix/cloudnav/internal/version"
)

const (
	keyEsc           = "esc"
	keyEnter         = "enter"
	keyUp            = "up"
	keyDown          = "down"
	statusCostCached = "cost column on (cached)"
	// currencyUSD is the fallback code used when a provider returns a
	// blank Currency field — shared between the two currency-symbol
	// helpers in this file.
	currencyUSD = "USD"
)

func Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newModel()
	m.ctx = ctx
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	// If the user hit R on the post-upgrade overlay we set m.relaunch
	// so that once bubbletea has torn down the alt-screen we can exec
	// the new binary cleanly, replacing this process with the upgraded
	// one. Otherwise the running image stays on the old version until
	// the user quits and re-runs manually.
	if fm, ok := final.(*model); ok && fm.relaunch {
		return execReplacement()
	}
	return nil
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

	healthLoadedMsg  struct{ events []provider.HealthEvent }
	metricsLoadedMsg struct {
		data  []provider.Metric
		label string
	}

	costHistoryLoadedMsg struct {
		history provider.CostHistory
		opts    provider.CostHistoryOptions
	}
	updateCheckMsg   struct{ result updatecheck.Result }
	upgradeStartMsg  struct{}
	upgradeResultMsg struct {
		summary string
		err     error
	}

	advisorLoadedMsg struct {
		recs      []provider.Recommendation
		scope     string
		scopeName string
	}
	loginDoneMsg     struct{ cloud string }
	loginStatusMsg   struct{ status map[string]string }
	billingLoadedMsg struct {
		lines     []provider.CostLine
		scope     string
		gcpStatus *gcp.BillingStatus
		summary   provider.BillingScope // account-wide forecast + budget; zero-value when the provider doesn't implement it
	}
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
	sortCategory
)

func (s sortMode) String() string {
	switch s {
	case sortState:
		return "state"
	case sortLocation:
		return "location"
	case sortCategory:
		return "category"
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
	ctx           context.Context
	providers     []provider.Provider
	active        provider.Provider
	stack         []frame
	visibleNodes  []provider.Node
	table         table.Model
	spinner       spinner.Model
	search        textinput.Model
	detail        viewport.Model
	detailTitle   string
	detailMode    bool
	searchMode    bool
	filter        string
	sort          sortMode
	loading       bool
	err           error
	status        string
	showHelp      bool
	paletteMode   bool
	paletteInput  textinput.Model
	paletteItems  []paletteItem
	paletteIdx    int
	cfg           *config.Config
	showCost      bool
	costs         map[string]map[string]string       // subID → lowercased rg name → cost
	tenantFilter  string                             // only show subs whose Meta[tenantName] == this (empty = all)
	locks         map[string]map[string][]azure.Lock // subID → rgName(lower) → locks
	selected      map[string]bool                    // node ID → selected
	restorePath   []config.Crumb                     // remaining crumbs to drill into during bookmark restore
	restoreLabel  string                             // label shown while restoring (for status)
	entities      map[string][]provider.Node         // provider name → top-level entities (subs/projects/accounts)
	pimMode       bool
	pimRoles      []provider.PIMRole
	pimCursor     int
	pimActivate   bool
	pimInput      textinput.Model
	pimFilter     string
	pimFilterOn   bool
	pimFilterIn   textinput.Model
	pimDuration   int
	pimSourceFilt string // "" = all, pimSrc{Azure,Entra,Group}
	advisorMode   bool
	advisorRecs   []provider.Recommendation
	advisorScope  string
	advisorName   string
	// advisorResource carries the row that was under the cursor when
	// advisor loaded. Populated only when we loaded for a specific
	// resource (not a whole subscription / account / project). Used
	// by the popup to render the resource-context header block.
	advisorResource provider.Node
	advisorIdx      int
	advisorFilter   string            // lowercased substring applied across category/impact/problem/target
	advisorFilterOn bool              // true while the user is typing in the filter input
	advisorFilterIn textinput.Model   // dedicated input; mirrors pimFilterIn
	loginStatus     map[string]string // providerName → human-readable auth state
	billingMode     bool
	billingLines    []provider.CostLine
	billingScope    string // provider name that produced billingLines
	billingIdx      int
	billingGCP      *gcp.BillingStatus    // optional GCP setup diagnostic
	billingSummary  provider.BillingScope // account-wide forecast + budget when the provider implements BillingSummarer
	healthMode      bool
	healthEvents    []provider.HealthEvent
	healthIdx       int
	metricsMode     bool
	metricsData     []provider.Metric
	metricsLabel    string // "resource-name · type" for the overlay header
	// Cost-history overlay (`$` key). Daily spend line chart over the last
	// stock-ticker style with W / M / 3M / 6M / Y window presets and
	// month-over-month delta annotations, for providers that implement
	// CostHistoryer.
	costHistMode    bool
	costHistData    provider.CostHistory
	costHistOpts    provider.CostHistoryOptions // currently-selected window + bucket
	costHistLoading bool
	// costHistSubs is the ordered list of subs the user can cycle
	// through while the chart overlay is open ([ / ] keys). Populated
	// at overlay-open time from the current stack frame or the entity
	// cache; empty means no sub cycling is available.
	costHistSubs   []provider.Node
	costHistSubIdx int
	// Upgrade prompt (`U` key). Populated from a background update check
	// on startup so the top-right "↑ update available" badge can
	// highlight when a newer tag is published on GitHub.
	updateAvailable bool
	latestVersion   string
	latestURL       string
	upgradeMode     bool // confirmation overlay visible
	upgradePlan     updatecheck.UpgradePlan
	upgradeRunning  bool
	upgradeResult   string
	upgradeErr      error
	drilling        bool   // a drill-level load is in flight; block navigation
	categoryFilter  string // resource category on the resource list (compute / data / network / security / other)
	deleteMode      bool
	deleteTargets   []provider.Node
	deleteScope     deleteScope
	// pendingDelete is a sticky confirmation shown in the footer so the
	// user can see that a delete request succeeded even after the
	// auto-refresh overwrites m.status. Cleared when they press esc or
	// the targeted rows disappear from the list.
	pendingDelete string
	// pendingDeleteUntil hides the banner after this moment so it
	// doesn't hang around forever — Azure usually finishes RG deletes
	// within a few minutes.
	pendingDeleteUntil time.Time
	deleteInput        textinput.Model
	width              int
	height             int
	keys               keys.Map
	// costCache persists cost results between runs so a second `c` press
	// after a restart serves from disk instead of repeating the 1–2s
	// Cost Management query.
	costCache *cache.Store[map[string]string]
	// pimCache persists PIM eligibilities across runs. The live fetch
	// across N tenants can take 5–15 s on cold cache (token
	// acquisition spawns az under the hood); a 5-min TTL makes repeat
	// opens in the same work block instant.
	pimCache *cache.Store[[]provider.PIMRole]
	// rgraphCache persists Azure Resource Graph snapshots — the
	// "drill into N RGs" KQL fan-out. A drill across 100+ RGs in a
	// large sub takes 2–5 s on cold cache; 10-min TTL makes repeat
	// drills in the same session instant. Keyed by (subID, sorted
	// RG names) so the cache survives changes in selection order.
	rgraphCache *cache.Store[[]provider.Node]
	// relaunch is set by the post-upgrade 'R' key. Run() inspects it
	// after the TUI quits and execs the freshly-installed cloudnav
	// binary in place of the current process.
	relaunch bool
	// autoUpgrading distinguishes a config-driven silent upgrade from
	// one the user initiated. On success the silent path re-execs
	// automatically so the user never sees the pill.
	autoUpgrading bool
	// term is the embedded PTY terminal page. Non-nil while the user
	// is shelled in via `x`. View() routes to it before any other
	// overlay so the terminal owns the whole screen.
	term *terminal
}

// newPromptInput builds a textinput with the shared theme. All prompts
// across the TUI are cyan bold (or red for destructive confirms).
func newPromptInput(prompt, placeholder string, charLimit int, promptStyle lipgloss.Style) textinput.Model {
	t := textinput.New()
	t.Prompt = prompt
	t.Placeholder = placeholder
	t.CharLimit = charLimit
	t.PromptStyle = promptStyle
	return t
}

func newModel() *model {
	// Load config first so the user's stored theme + spinner choice
	// applies before any styled object is built. Theme switches at
	// runtime call styles.Apply() again; this is the bootstrap pass.
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Theme != "" {
		if t, ok := styles.UIThemeByName(cfg.Theme); ok {
			styles.Apply(t)
		}
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	if cfg.Spinner != "" {
		if s, ok := styles.SpinnerByName(cfg.Spinner); ok {
			sp.Spinner = s
		}
	}
	sp.Style = styles.Spinner

	t := table.New(
		table.WithFocused(true),
		table.WithHeight(20),
	)
	ts := table.DefaultStyles()
	ts.Header, ts.Selected, ts.Cell = styles.TableStyles()
	t.SetStyles(ts)

	ti := newPromptInput("/ ", "filter by name", 120, styles.Prompt)
	pi := newPromptInput(": ", "search any sub / project / account, or switch cloud, or jump to bookmark", 120, styles.Prompt)
	pimIn := newPromptInput("justification: ", "e.g. investigating prod incident INC-4812", 200, styles.Prompt)
	pimFilt := newPromptInput("filter PIM: ", "tenant, subscription, or role...", 120, styles.Prompt)
	advFilt := newPromptInput("filter Advisor: ", "cost / security / high / sql / ...", 120, styles.Prompt)
	delIn := newPromptInput("type DELETE to confirm: ", "DELETE", 16, styles.PromptErr)

	vp := viewport.New(80, 20)

	// Install the FX converter once at bootstrap. nil-safe — when
	// the user hasn't set display_currency the converter is still
	// constructed but every Convert call passes the amount through
	// unchanged. That keeps the formatters branchless.
	currency.SetDefault(currency.New(cfg.DisplayCurrency))

	m := &model{
		ctx:             context.Background(),
		providers:       buildProviders(cfg),
		spinner:         sp,
		search:          ti,
		paletteInput:    pi,
		pimInput:        pimIn,
		pimFilterIn:     pimFilt,
		advisorFilterIn: advFilt,
		deleteInput:     delIn,
		pimDuration:     8,
		detail:          vp,
		cfg:             cfg,
		costs:           map[string]map[string]string{},
		entities:        map[string][]provider.Node{},
		locks:           map[string]map[string][]azure.Lock{},
		selected:        map[string]bool{},
		loginStatus:     map[string]string{},
		keys:            keys.Default(),
		table:           t,
		showCost:        true,
	}
	// All caches — cost / pim / rgraph here, plus the Azure root and
	// update-check stores in their respective packages — share the
	// process-wide backend resolved by cache.Shared(). One open
	// SQLite handle, one set of WAL files.
	backend := cache.Shared()
	// 15-minute TTL is a balance: long enough that flipping between
	// views within a session stays instant, short enough that a new
	// purchase / new resource shows up after a refresh without the
	// user having to press X (clear cache).
	m.costCache = cache.NewStoreWithBackend[map[string]string](backend, "costs", 15*time.Minute)
	m.pimCache = cache.NewStoreWithBackend[[]provider.PIMRole](backend, "pim", 5*time.Minute)
	m.rgraphCache = cache.NewStoreWithBackend[[]provider.Node](backend, "rgraph", 10*time.Minute)
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
	return tea.Batch(m.spinner.Tick, m.checkLogins(), m.loadUpdateCheck())
}

// checkLogins pings each provider's LoggedIn() concurrently and reports back
// via loginStatusMsg so the home cloud list can badge each row with the
// user's current auth state. Purely informational — drilling into a cloud
// still triggers Root() which surfaces fresh errors.

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Terminal page owns the screen while it's open. We still let the
	// window-size message fall through to the navigator below so the
	// cached width/height stay in sync — the terminal forwards its
	// own copy via tea.WindowSizeMsg too.
	if m.term != nil {
		switch msg := msg.(type) {
		case termPaintMsg, termExitMsg:
			next, cmd := m.term.Update(msg)
			m.term = next
			if next == nil {
				m.status = "✓ terminal closed"
			}
			return m, cmd
		case tea.KeyMsg:
			next, cmd := m.term.Update(msg)
			m.term = next
			if next == nil {
				m.status = "✓ terminal closed"
			}
			return m, cmd
		case tea.WindowSizeMsg:
			next, cmd := m.term.Update(msg)
			m.term = next
			// Fall through so navigator chrome stays sized too.
			m.width, m.height = msg.Width, msg.Height
			if w := msg.Width; w > 0 {
				m.table.SetWidth(w)
				m.search.Width = w - 4
				m.detail.Width = w
			}
			m.applyChromeHeight()
			m.refreshTable()
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if w := msg.Width; w > 0 {
			m.table.SetWidth(w)
			m.search.Width = w - 4
			m.detail.Width = w
		}
		m.applyChromeHeight()
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
		if m.healthMode {
			return m.updateHealth(msg)
		}
		if m.metricsMode {
			return m.updateMetrics(msg)
		}
		if m.billingMode {
			return m.updateBilling(msg)
		}
		if m.costHistMode {
			return m.updateCostHistory(msg)
		}
		if m.upgradeMode {
			return m.updateUpgrade(msg)
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
				m.setCategoryFilter("")
				return m, nil
			case "1":
				m.setCategoryFilter(catCompute)
				return m, nil
			case "2":
				m.setCategoryFilter(catData)
				return m, nil
			case "3":
				m.setCategoryFilter(catNetwork)
				return m, nil
			case "4":
				m.setCategoryFilter(catSecurity)
				return m, nil
			case "5":
				m.setCategoryFilter(catOther)
				return m, nil
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
			m.sort = (m.sort + 1) % 4
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
		case key.Matches(msg, m.keys.Health):
			return m, m.loadHealth()
		case key.Matches(msg, m.keys.Metrics):
			return m, m.loadMetrics()
		case key.Matches(msg, m.keys.Billing):
			return m, m.loadBilling()
		case key.Matches(msg, m.keys.CostHistory):
			return m, m.loadCostHistory(defaultCostWindow())
		case key.Matches(msg, m.keys.Upgrade):
			return m, m.openUpgrade()
		case key.Matches(msg, m.keys.Exec):
			return m, m.execShell()
		case key.Matches(msg, m.keys.Enter):
			return m, m.drillDown()
		case key.Matches(msg, m.keys.Back):
			// esc dismisses the sticky deletion banner before popping
			// the nav stack, so a single tap clears the confirmation
			// without navigating away.
			if m.pendingDelete != "" {
				m.pendingDelete = ""
				m.pendingDeleteUntil = time.Time{}
				return m, nil
			}
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
		// Sticky banner — survives the follow-up reload so the user
		// sees the confirmation even after the table re-renders.
		// 10 minutes covers the typical Azure RG teardown; after that
		// the row is usually gone anyway.
		m.pendingDelete = "✓ " + msg.msg + " — watch the STATE column for 'Deleting'"
		m.pendingDeleteUntil = time.Now().Add(10 * time.Minute)
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
		// Persist to disk so a second cloudnav run doesn't repeat the
		// Cost Management query. Best-effort; ignore errors.
		if m.costCache != nil && len(msg.costs) > 0 {
			_ = m.costCache.Set(msg.parentID, msg.costs)
		}
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

	case billingLoadedMsg:
		m.loading = false
		m.err = nil
		// Sort descending by current spend so the biggest line items land on top.
		sort.SliceStable(msg.lines, func(i, j int) bool { return msg.lines[i].Current > msg.lines[j].Current })
		m.billingLines = msg.lines
		m.billingScope = msg.scope
		m.billingGCP = msg.gcpStatus
		m.billingSummary = msg.summary
		m.billingIdx = 0
		m.billingMode = true
		m.status = fmt.Sprintf("%d billing line(s) for %s", len(msg.lines), msg.scope)
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

	case healthLoadedMsg:
		m.loading = false
		m.err = nil
		m.healthEvents = msg.events
		m.healthIdx = 0
		m.healthMode = true
		m.status = fmt.Sprintf("%d active health event(s)", len(msg.events))
		return m, nil

	case metricsLoadedMsg:
		m.loading = false
		m.err = nil
		m.metricsData = msg.data
		m.metricsLabel = msg.label
		m.metricsMode = true
		m.status = fmt.Sprintf("%d metric series for %s", len(msg.data), msg.label)
		return m, nil

	case costHistoryLoadedMsg:
		m.loading = false
		m.costHistLoading = false
		m.err = nil
		m.costHistData = msg.history
		m.costHistOpts = msg.opts
		m.costHistMode = true
		m.status = fmt.Sprintf("cost history · %s · %d point(s)", windowLabel(msg.opts), len(msg.history.Series.Points))
		return m, nil

	case updateCheckMsg:
		wasChecking := strings.HasPrefix(m.status, "checking GitHub")
		m.updateAvailable = msg.result.Available
		m.latestVersion = msg.result.Latest
		m.latestURL = msg.result.URL
		// Opt-in auto-upgrade: if the user turned on AutoUpgrade in config
		// AND a newer release is available AND the plan is non-interactive
		// (go install / brew — we never auto-launch a browser), run it now
		// without opening the confirm overlay.
		if msg.result.Available && m.cfg != nil && m.cfg.AutoUpgrade && !m.upgradeRunning && m.upgradeResult == "" {
			plan := updatecheck.PlanUpgrade(m.latestVersion, m.latestURL)
			if plan.Method == updatecheck.UpgradeGoInstall || plan.Method == updatecheck.UpgradeHomebrew {
				m.upgradePlan = plan
				m.upgradeRunning = true
				m.autoUpgrading = true
				m.status = "auto-upgrading to " + m.latestVersion + "..."
				return m, m.runUpgrade()
			}
		}
		// If the user explicitly asked for a check via the U shortcut,
		// close the loop: open the upgrade confirm when something is
		// available, otherwise tell them we looked and they're current.
		if wasChecking {
			if msg.result.Available {
				return m, m.openUpgrade()
			}
			if msg.result.Err != nil {
				m.status = "couldn't reach GitHub — " + firstErrLine(msg.result.Err)
			} else {
				m.status = "already on the latest release (" + version.Version + ")"
			}
		}
		return m, nil

	case upgradeStartMsg:
		m.upgradeRunning = true
		m.status = "upgrading cloudnav..."
		return m, m.runUpgrade()

	case upgradeResultMsg:
		m.upgradeRunning = false
		m.upgradeResult = msg.summary
		m.upgradeErr = msg.err
		if msg.err == nil {
			m.updateAvailable = false
			m.status = "✓ upgrade complete — restart cloudnav to use " + m.latestVersion
			// Autonomous path: if this was a silent auto-upgrade
			// (config.AutoUpgrade true, no user interaction), hand off
			// to the new binary immediately. The user sees one "auto-
			// upgrading" flash, then cloudnav reopens on the new
			// version with the pill already gone.
			if m.autoUpgrading {
				m.autoUpgrading = false
				m.relaunch = true
				return m, tea.Quit
			}
		} else {
			m.status = "upgrade failed"
			m.autoUpgrading = false
		}
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
		// Refresh the disk cache with the new Active flag so a restart
		// doesn't re-show the role as pending activation.
		if m.pimCache != nil && m.active != nil && len(m.pimRoles) > 0 {
			_ = m.pimCache.Set(m.active.Name()+":roles", m.pimRoles)
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

// loadBilling fires the active provider's Billing() call and opens the
// billing overlay. Implements the `B` key. Falls through with a status hint
// when the active provider doesn't implement provider.Billing. For GCP we
// also pull BillingStatus so the overlay can render a setup checklist when
// the BQ export isn't live yet.

// loadMetrics pulls a short-window time-series for the resource under
// the cursor. Per-resource only; the caller must be on the resource
// view. Uses provider.Metricser when the active cloud implements it.

// loadHealth opens the Service Health overlay, showing active incidents
// affecting the caller's scope. Optional provider capability — we show a
// status hint when the active cloud doesn't implement HealthEventer.

// loadAdvisor fetches cloud-native advisor recommendations for the scope
// under the cursor and opens the advisor overlay. Azure goes to ARM Advisor,
// GCP goes to Cloud Recommender. The provider.Advisor interface lets future
// clouds drop in without touching the TUI.

// loginCurrentCloud runs the active cloud's CLI login interactively. Suspends
// the TUI via tea.ExecProcess so the browser redirect / device-code prompt
// the cloud CLI prints land in the user's terminal. On return the TUI
// refreshes so the cloud's nodes populate without a manual relaunch. When
// the CLI itself is missing we run the install plan first (single TUI
// suspension), then fall through to the login step.

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
	// Keybar length can change on drill (new pairs become available) —
	// re-apply chrome height so the wrapped keybar doesn't eat table rows.
	m.applyChromeHeight()
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
	case sortCategory:
		// Group by category (compute → container → data → network →
		// security → other), then by name inside each bucket.
		sort.SliceStable(out, func(i, j int) bool {
			ci := categorySortOrder(typeColorCategory(out[i]))
			cj := categorySortOrder(typeColorCategory(out[j]))
			if ci != cj {
				return ci < cj
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
	default:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
	}
	return out
}

// setCategoryFilter updates the active resource-category filter and triggers
// a table refresh.
func (m *model) setCategoryFilter(cat string) {
	m.categoryFilter = cat
	if cat == "" {
		m.status = "category: all"
	} else {
		m.status = "category: " + cat
	}
	m.refreshTable()
}

// Resource category constants used by the category filter bar on the
// resource-list view. Kept short so the tab row stays readable.
// The filter bar buckets container resources under 'compute' so users
// don't juggle seven tabs; the colour bar on the TYPE cell splits
// them out (see typeColorCategory) for quick visual scanning.
const (
	catCompute   = "compute"
	catContainer = "container"
	catData      = "data"
	catNetwork   = "network"
	catSecurity  = "security"
	catOther     = "other"
)

// resourceCategory sorts a Node into one of ~5 buckets based on its type
// string. The mapping is deliberately coarse — users want "show me compute"
// not a 30-way faceted search — and covers Azure / GCP / AWS with the same
// function so the TUI tab bar stays provider-agnostic.
func resourceCategory(n provider.Node) string {
	t := strings.ToLower(n.Meta["type"])
	switch {
	// Compute (VMs, serverless, containers, batch, HPC)
	case strings.Contains(t, "microsoft.compute/"),
		strings.Contains(t, "microsoft.containerservice"),
		strings.Contains(t, "microsoft.containerinstance"),
		strings.Contains(t, "microsoft.containerregistry"),
		strings.Contains(t, "microsoft.web/"),
		strings.Contains(t, "microsoft.batch"),
		strings.Contains(t, "microsoft.app/"),
		strings.Contains(t, "microsoft.desktopvirtualization"),
		strings.Contains(t, "microsoft.hybridcompute"),
		strings.Contains(t, "microsoft.kubernetes"),
		strings.Contains(t, "microsoft.logic"),
		strings.Contains(t, "microsoft.machinelearningservices"),
		strings.Contains(t, "microsoft.hdinsight"),
		strings.Contains(t, "microsoft.databricks"),
		strings.Contains(t, "microsoft.servicefabric"),
		strings.Contains(t, "compute.googleapis.com"),
		strings.Contains(t, "container.googleapis.com"),
		strings.Contains(t, "run.googleapis.com"),
		strings.Contains(t, "cloudfunctions.googleapis.com"),
		strings.Contains(t, "appengine.googleapis.com"),
		strings.Contains(t, "workflows.googleapis.com"),
		strings.Contains(t, "artifactregistry.googleapis.com"),
		strings.Contains(t, "cloudbuild.googleapis.com"),
		strings.Contains(t, "composer.googleapis.com"),
		strings.Contains(t, "aiplatform.googleapis.com"),
		strings.Contains(t, "notebooks.googleapis.com"),
		strings.HasPrefix(t, "ec2:"),
		strings.HasPrefix(t, "lambda:"),
		strings.HasPrefix(t, "ecs:"),
		strings.HasPrefix(t, "eks:"),
		strings.HasPrefix(t, "batch:"),
		strings.HasPrefix(t, "ecr:"),
		strings.HasPrefix(t, "autoscaling:"),
		strings.HasPrefix(t, "apprunner:"),
		strings.HasPrefix(t, "elasticbeanstalk:"),
		strings.HasPrefix(t, "codebuild:"),
		strings.HasPrefix(t, "codedeploy:"),
		strings.HasPrefix(t, "codepipeline:"),
		strings.HasPrefix(t, "sagemaker:"),
		strings.HasPrefix(t, "amplify:"):
		return catCompute

	// Data (relational, NoSQL, cache, analytics, object storage, streaming)
	case strings.Contains(t, "microsoft.sql"),
		strings.Contains(t, "microsoft.storage"),
		strings.Contains(t, "microsoft.documentdb"),
		strings.Contains(t, "microsoft.cache"),
		strings.Contains(t, "microsoft.dbforpostgresql"),
		strings.Contains(t, "microsoft.dbformysql"),
		strings.Contains(t, "microsoft.dbformariadb"),
		strings.Contains(t, "microsoft.synapse"),
		strings.Contains(t, "microsoft.datafactory"),
		strings.Contains(t, "microsoft.datalakestore"),
		strings.Contains(t, "microsoft.datalakeanalytics"),
		strings.Contains(t, "microsoft.streamanalytics"),
		strings.Contains(t, "microsoft.eventhub"),
		strings.Contains(t, "microsoft.servicebus"),
		strings.Contains(t, "microsoft.eventgrid"),
		strings.Contains(t, "microsoft.search"),
		strings.Contains(t, "microsoft.purview"),
		strings.Contains(t, "microsoft.insights"),
		strings.Contains(t, "microsoft.operationalinsights"),
		strings.Contains(t, "sqladmin.googleapis.com"),
		strings.Contains(t, "spanner.googleapis.com"),
		strings.Contains(t, "bigtable"),
		strings.Contains(t, "redis.googleapis.com"),
		strings.Contains(t, "memcache.googleapis.com"),
		strings.Contains(t, "firestore.googleapis.com"),
		strings.Contains(t, "datastore.googleapis.com"),
		strings.Contains(t, "storage.googleapis.com"),
		strings.Contains(t, "bigquery.googleapis.com"),
		strings.Contains(t, "dataflow.googleapis.com"),
		strings.Contains(t, "dataproc.googleapis.com"),
		strings.Contains(t, "dataplex.googleapis.com"),
		strings.Contains(t, "pubsub.googleapis.com"),
		strings.Contains(t, "monitoring.googleapis.com"),
		strings.Contains(t, "logging.googleapis.com"),
		strings.Contains(t, "filestore.googleapis.com"),
		strings.HasPrefix(t, "s3:"),
		strings.HasPrefix(t, "rds:"),
		strings.HasPrefix(t, "dynamodb:"),
		strings.HasPrefix(t, "dax:"),
		strings.HasPrefix(t, "elasticache:"),
		strings.HasPrefix(t, "redshift:"),
		strings.HasPrefix(t, "opensearch:"),
		strings.HasPrefix(t, "es:"),
		strings.HasPrefix(t, "glue:"),
		strings.HasPrefix(t, "athena:"),
		strings.HasPrefix(t, "kinesis:"),
		strings.HasPrefix(t, "firehose:"),
		strings.HasPrefix(t, "timestream:"),
		strings.HasPrefix(t, "efs:"),
		strings.HasPrefix(t, "fsx:"),
		strings.HasPrefix(t, "backup:"),
		strings.HasPrefix(t, "glacier:"),
		strings.HasPrefix(t, "sns:"),
		strings.HasPrefix(t, "sqs:"),
		strings.HasPrefix(t, "events:"),
		strings.HasPrefix(t, "stepfunctions:"),
		strings.HasPrefix(t, "msk:"),
		strings.HasPrefix(t, "mq:"),
		strings.HasPrefix(t, "cloudwatch:"),
		strings.HasPrefix(t, "logs:"):
		return catData

	// Network (VPC, DNS, CDN, load balancers, firewalls, endpoints)
	case strings.Contains(t, "microsoft.network"),
		strings.Contains(t, "microsoft.cdn"),
		strings.Contains(t, "microsoft.communication"),
		strings.Contains(t, "microsoft.apimanagement"),
		strings.Contains(t, "microsoft.signalrservice"),
		strings.Contains(t, "dns.googleapis.com"),
		strings.Contains(t, "networkconnectivity.googleapis.com"),
		strings.Contains(t, "servicedirectory.googleapis.com"),
		strings.Contains(t, "vpcaccess.googleapis.com"),
		strings.HasPrefix(t, "elasticloadbalancing:"),
		strings.HasPrefix(t, "elbv2:"),
		strings.HasPrefix(t, "route53:"),
		strings.HasPrefix(t, "apigateway:"),
		strings.HasPrefix(t, "apigatewayv2:"),
		strings.HasPrefix(t, "cloudfront:"),
		strings.HasPrefix(t, "vpc:"),
		strings.HasPrefix(t, "directconnect:"),
		strings.HasPrefix(t, "globalaccelerator:"),
		strings.HasPrefix(t, "appsync:"),
		strings.HasPrefix(t, "appmesh:"),
		strings.HasPrefix(t, "transfer:"):
		return catNetwork

	// Security (IAM, secrets, KMS, policy, identity, WAF, compliance)
	case strings.Contains(t, "microsoft.keyvault"),
		strings.Contains(t, "microsoft.managedidentity"),
		strings.Contains(t, "microsoft.security"),
		strings.Contains(t, "microsoft.policyinsights"),
		strings.Contains(t, "microsoft.authorization"),
		strings.Contains(t, "microsoft.dataprotection"),
		strings.Contains(t, "iam.googleapis.com"),
		strings.Contains(t, "secretmanager.googleapis.com"),
		strings.Contains(t, "cloudkms.googleapis.com"),
		strings.Contains(t, "privateca.googleapis.com"),
		strings.Contains(t, "certificatemanager.googleapis.com"),
		strings.Contains(t, "binaryauthorization.googleapis.com"),
		strings.Contains(t, "beyondcorp.googleapis.com"),
		strings.Contains(t, "iap.googleapis.com"),
		strings.HasPrefix(t, "iam:"),
		strings.HasPrefix(t, "kms:"),
		strings.HasPrefix(t, "secretsmanager:"),
		strings.HasPrefix(t, "acm:"),
		strings.HasPrefix(t, "wafv2:"),
		strings.HasPrefix(t, "cognito-idp:"),
		strings.HasPrefix(t, "guardduty:"),
		strings.HasPrefix(t, "config:"),
		strings.HasPrefix(t, "cloudtrail:"),
		strings.HasPrefix(t, "ssm:"):
		return catSecurity
	}
	return catOther
}

// typeColorCategory returns the category we colour the TYPE cell
// with. Finer-grained than resourceCategory (container split from
// compute) so container resources (AKS / ACR / GKE / ECS / EKS) read
// differently on the list than pure VMs — makes visual scanning of a
// mixed resource group much faster.
func typeColorCategory(n provider.Node) string {
	t := strings.ToLower(n.Meta["type"])
	switch {
	case strings.Contains(t, "microsoft.containerservice"),
		strings.Contains(t, "microsoft.containerregistry"),
		strings.Contains(t, "microsoft.containerinstance"),
		strings.Contains(t, "microsoft.app/"),
		strings.Contains(t, "container.googleapis.com"),
		strings.Contains(t, "run.googleapis.com"),
		strings.Contains(t, "artifactregistry.googleapis.com"),
		strings.HasPrefix(t, "ecs:"),
		strings.HasPrefix(t, "eks:"),
		strings.HasPrefix(t, "ecr:"),
		strings.HasPrefix(t, "apprunner:"):
		return catContainer
	}
	return resourceCategory(n)
}

// categoryStyle returns the colour used for each category on the TYPE
// cell. Six buckets, one colour each — enough to scan a mixed list
// quickly without overwhelming the eye.
func categoryStyle(cat string) lipgloss.Style {
	switch cat {
	case catCompute:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("114")) // green — VMs, batch
	case catContainer:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // purple — AKS, ECR, GKE
	case catData:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("215")) // amber — storage, dbs
	case catNetwork:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // sky — vnet, elb
	case catSecurity:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("210")) // rose — KV, IAM, WAF
	default:
		return lipgloss.NewStyle().Foreground(styles.Subtle)
	}
}

// categorySortOrder maps the category name to a stable display order
// used by the 's' sort mode: compute → container → data → network →
// security → other. Name sort applies within each bucket.
func categorySortOrder(cat string) int {
	switch cat {
	case catCompute:
		return 0
	case catContainer:
		return 1
	case catData:
		return 2
	case catNetwork:
		return 3
	case catSecurity:
		return 4
	default:
		return 5
	}
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
	case provider.KindFolder:
		return []table.Column{
			{Title: "NAME", Width: 42},
			{Title: "FOLDER ID", Width: 28},
			{Title: "STATE", Width: 12},
		}
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
			{Title: "TAGS", Width: tagsColWidth},
		}
		if m.showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: 20})
		}
		return cols
	case provider.KindResource:
		// Resource view has many columns; the COST column sits last and
		// would otherwise be the first to clip on narrow terminals. We
		// give COST a fixed budget (20) and compress the middle columns
		// to fit whatever width remains. Terminal widths we target:
		//  • 120 cells: drop HEALTH, compress TAGS.
		//  • 140 cells: standard layout.
		//  • 160+ cells: generous TAGS.
		showCost := m.showCost && m.active != nil && m.active.Name() == pimSrcAzure
		showAgg := f.aggregated
		w := m.width
		if w == 0 {
			w = 160
		}
		// Budget the fixed columns first.
		costW := 0
		if showCost {
			costW = 20
		}
		aggW := 0
		if showAgg {
			aggW = 28
		}
		// Reserve fixed columns + 2-cell padding between 8 columns.
		fixed := 4 /*sel*/ + 40 /*name*/ + costW + aggW + 2*8
		remaining := w - fixed
		if remaining < 40 {
			remaining = 40
		}
		// Split the remaining budget across type/location/created/health/tags
		// proportionally. Health gets dropped first on narrow layouts.
		typeW, locW, createdW, healthW, tagsW := splitResourceCols(remaining)
		cols := []table.Column{
			{Title: " ", Width: 4},
			{Title: "NAME", Width: 40},
			{Title: "TYPE", Width: typeW},
			{Title: "LOCATION", Width: locW},
			{Title: "CREATED", Width: createdW},
		}
		if healthW > 0 {
			cols = append(cols, table.Column{Title: "HEALTH", Width: healthW})
		}
		cols = append(cols, table.Column{Title: "TAGS", Width: tagsW})
		if showAgg {
			cols = append(cols, table.Column{Title: "RESOURCE GROUP", Width: aggW})
		}
		// Only Azure exposes a per-resource cost API. GCP's BigQuery billing
		// export doesn't reliably surface a resource_name column, and AWS CE
		// groups by service/region not individual resources, so the column
		// would just be "—" everywhere.
		if showCost {
			cols = append(cols, table.Column{Title: "COST (MTD)", Width: costW})
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
		case provider.KindFolder:
			rows = append(rows, table.Row{n.Name, n.ID, n.State})
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
			row := table.Row{selectionMark(m.selected[n.ID]), n.Name, n.Location, stateBadge(n.State), lock, shortenTags(n.Meta["tags"], tagsColWidth-1)}
			if m.showCost {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		case provider.KindResource:
			// The HEALTH column is conditionally dropped on narrow
			// terminals (see splitResourceCols). Mirror that choice
			// here so row length matches column count — bubbles/table
			// renders blank cells when they fall short, but a mismatched
			// extra cell overflows the last visible column.
			showHealth := true
			w := m.width
			if w > 0 {
				costW := 0
				if m.showCost && m.active != nil && m.active.Name() == pimSrcAzure {
					costW = 20
				}
				aggW := 0
				if len(m.stack) > 0 && m.stack[len(m.stack)-1].aggregated {
					aggW = 28
				}
				budget := w - (4 + 40 + costW + aggW + 2*8)
				if budget < 80 {
					showHealth = false
				}
			}
			row := table.Row{
				selectionMark(m.selected[n.ID]),
				n.Name,
				categoryStyle(typeColorCategory(n)).Render(friendlyType(n.Meta["type"])),
				n.Location,
				shortDate(n.Meta["createdTime"]),
			}
			if showHealth {
				row = append(row, healthBadge(n.Meta["health"]))
			}
			row = append(row, shortenTags(n.Meta["tags"], tagsColWidth-1))
			if len(m.stack) > 0 && m.stack[len(m.stack)-1].aggregated {
				row = append(row, n.Meta["originRG"])
			}
			if m.showCost && m.active != nil && m.active.Name() == pimSrcAzure {
				row = append(row, costOrDash(n.Cost))
			}
			rows = append(rows, row)
		default:
			rows = append(rows, table.Row{n.Name})
		}
	}
	return rows
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
	pimSrcAWSSSO    = "aws-sso"
	cliNotInstalled = "✗ CLI not installed"
	providerAzure   = "azure"
	providerGCP     = "gcp"
	providerAWS     = "aws"
	// tagsColWidth bounds the TAGS column on the RG and resource views.
	// Wide enough to read the first key=value pair; longer strings get a
	// trailing "…" so the row stays single-line.
	tagsColWidth = 22
	// healthColWidth fits the emoji + short label — "🟡 Degraded" — so the
	// column looks aligned whether the row is healthy (blank) or not.
	healthColWidth = 14
)

// splitResourceCols divides the remaining width budget among the flexible
// resource-view columns. TAGS is last in priority: it gets whatever's
// left after type/location/created/health are sized. HEALTH is dropped
// entirely (width 0) when the budget is too tight to show it readably.
func splitResourceCols(budget int) (typeW, locW, createdW, healthW, tagsW int) {
	// Typical layouts:
	//  • budget ≥ 100: generous — type 30, loc 14, created 12, health 14, tags 22+
	//  • budget ≥ 80:  compressed but all columns visible
	//  • budget <  80: drop HEALTH, keep the rest tight
	switch {
	case budget >= 100:
		typeW, locW, createdW, healthW = 30, 14, 12, healthColWidth
	case budget >= 80:
		typeW, locW, createdW, healthW = 26, 12, 10, healthColWidth
	case budget >= 60:
		typeW, locW, createdW, healthW = 22, 10, 10, 0
	default:
		typeW, locW, createdW, healthW = 18, 8, 10, 0
	}
	used := typeW + locW + createdW + healthW
	tagsW = budget - used
	if tagsW < 10 {
		tagsW = 10
	}
	if tagsW > tagsColWidth {
		tagsW = tagsColWidth
	}
	return
}

// healthBadge renders the per-resource availability status returned by
// Azure Resource Health. The provider only stores non-Available states
// in Meta so Available / blank rows render as em-dash without needing a
// lookup cycle. Unknown stays blank as well — surfacing it on every
// resource that Resource Health hasn't classified yet would drown out
// actual degraded signals.
// stateBadge colours the STATE column so the "Deleting" rows jump out
// of the list after a delete request. Azure returns provisioningState
// verbatim, so the match is on the raw string.
func stateBadge(state string) string {
	// Keep the text short and predictable. bubbles/table's cell
	// truncation walks the string as runes, not as ANSI tokens, so a
	// styled badge longer than the column cell would get chopped mid
	// escape and leak visible "…──0m" fragments. Plain 'Deleting' fits
	// the 12-cell STATE column with slack; users still see the state
	// change against the surrounding Succeeded rows.
	switch strings.ToLower(state) {
	case "deleting":
		return styles.WarnS.Bold(true).Render("Deleting")
	case "failed":
		return styles.Bad.Render(state)
	case "canceled", "cancelled":
		return styles.Help.Render(state)
	default:
		return state
	}
}

func healthBadge(state string) string {
	switch state {
	case "Unavailable":
		return styles.Bad.Render("🔴 Unavailable")
	case "Degraded":
		return styles.WarnS.Render("🟡 Degraded")
	case "Available":
		return styles.Good.Render("🟢 Available")
	default:
		return emDash
	}
}

// shortenTags renders a pre-formatted "k=v, k=v" tag string so it fits in
// the TAGS column. Empty input renders as the em-dash placeholder so the
// column doesn't look broken for untagged resources.
func shortenTags(s string, max int) string {
	if s == "" {
		return emDash
	}
	if max <= 1 {
		return "…"
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

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

// fullScreenBox returns a rounded-border container sized to fill the
// terminal. Used by information-dense overlays (advisor, pim, billing)
// that need the whole screen to render readable rows.
func fullScreenBox(width, height int) lipgloss.Style {
	w := width - 6
	if w < 40 {
		w = 40
	}
	h := height - 4
	if h < 10 {
		h = 10
	}
	return styles.Box.Width(w).Height(h)
}

// overlay renders content as a centered popup composited over the
// current list view. Unlike a plain Shell-swap, the table stays visible
// behind the modal (aztimator-style z-order) thanks to the ANSI-aware
// compositor. Use for compact dialogs like delete-confirm, help,
// upgrade, and palette; information-dense screens can still use
// fullScreenBox.
func (m *model) overlay(body string) string {
	bg := m.listBackground()
	if bg == "" {
		// Fall back to the old non-composited layout when we don't have
		// list state to draw on (e.g. before the first window size).
		header := m.headerView()
		footer := m.footerView()
		bodyH := m.height - lipgloss.Height(header) - 1
		if bodyH < 5 {
			bodyH = 5
		}
		placed := components.Modal(m.width, bodyH, body)
		return components.Shell(m.width, m.height, header, placed, footer)
	}
	return components.CenterOverlay(m.width, m.height, bg, body)
}

// listBackground renders the list view (header + table + footer) that
// any overlay sits on top of. Kept separate from View() so the compositor
// can sample it without triggering the overlay branch recursively.
func (m *model) listBackground() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	body := m.table.View()
	if m.isDrillLoading() {
		body = m.drillLoadingBody()
	} else if len(m.visibleNodes) == 0 && !m.loading && m.categoryFilter == "" {
		body = m.emptyBody()
	}
	header := m.headerView()
	if bar := m.categoryBar(); bar != "" {
		header = header + "\n" + bar
	}
	return components.Shell(m.width, m.height, header, body, m.footerView())
}

func (m *model) View() string {
	// Embedded terminal owns the whole screen — no navigator chrome,
	// no overlays. The terminal renders its own per-cloud frame.
	if m.term != nil {
		return m.term.View()
	}
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
	if m.healthMode {
		return m.healthView()
	}
	if m.metricsMode {
		return m.metricsView()
	}
	if m.billingMode {
		return m.billingView()
	}
	if m.costHistMode {
		return m.costHistoryView()
	}
	if m.upgradeMode {
		return m.upgradeView()
	}
	if m.paletteMode {
		return m.paletteView()
	}
	if m.detailMode {
		return components.Shell(m.width, m.height,
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
	header := m.headerView()
	if bar := m.categoryBar(); bar != "" {
		header = header + "\n" + bar
	}
	return components.Shell(m.width, m.height, header, body, m.footerView())
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
			return styles.TabActive.Render(text)
		}
		return styles.Tab.Render(text)
	}
	tabs := strings.Join([]string{
		tab("0", "all", "", len(top.nodes)),
		tab("1", "compute", catCompute, counts[catCompute]),
		tab("2", "data", catData, counts[catData]),
		tab("3", "network", catNetwork, counts[catNetwork]),
		tab("4", "security", catSecurity, counts[catSecurity]),
		tab("5", "other", catOther, counts[catOther]),
	}, " ")
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

// updateHealth handles keys while the Service Health overlay is visible.
// Kept small because the overlay is read-only — the only actions are
// navigation and dismiss.

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
	title := components.Breadcrumb("cloudnav", []string{"detail › " + m.detailTitle})
	right := styles.Help.Render(fmt.Sprintf("%d%%", int(m.detail.ScrollPercent()*100)))
	return components.TwoCol(m.width, title, right)
}

func (m *model) detailFooter() string {
	hints := strings.Join([]string{
		components.KeyPair("↑↓", "scroll"),
		components.KeyPair("esc", "close"),
		components.KeyPair("q", "close"),
	}, "  ")
	return styles.StatusBar.Render(hints)
}

func (m *model) headerView() string {
	crumb := components.Breadcrumb("cloudnav", breadcrumbs(m.stack))
	right := m.updateIndicator()
	// Paint both lines with the HeaderBar background so the breadcrumb +
	// keybar form a single visual zone. Width is the terminal width so
	// the dark strip reaches edge-to-edge.
	w := m.width
	if w <= 0 {
		w = 120
	}
	top := styles.HeaderBar.Width(w).Render(components.TwoCol(w-2, crumb, right))
	bar := styles.HeaderBar.Width(w).Render(m.keybar())
	return top + "\n" + bar + "\n"
}

// updateIndicator renders the top-right badge. When a newer release is
// live on GitHub we render a loud yellow-bg pill so it's immediately
// obvious from across the room; clicking through is one key press (U).
// Otherwise we show the current version quietly with a trailing smile.
func (m *model) updateIndicator() string {
	if m.updateAvailable {
		tag := m.latestVersion
		if tag == "" {
			tag = "new"
		}
		// Reversed-video pill (yellow bg, dark fg) so the badge pops
		// against every theme — dark and light. Bracketed so it reads
		// as an actionable chip, and the U binding is in the label.
		return updatePillStyle.Render(" ↑ " + tag + " available — press U ")
	}
	v := version.Version
	if v == "dev" || v == "" {
		return styles.Help.Render("^_^")
	}
	return styles.Help.Render(v + " ^_^")
}

// updatePillStyle is the loud top-right "update available" pill. Kept
// as a package-level var so we build it once — lipgloss styles are
// cheap but the badge is rendered on every frame.
var updatePillStyle = lipgloss.NewStyle().
	Background(styles.Warn).
	Foreground(lipgloss.Color("#111827")).
	Bold(true)

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
	if _, ok := m.active.(provider.Billing); ok && m.active != nil {
		pairs = append(pairs, pair{"B", "billing"})
	}
	if _, ok := m.active.(provider.CostHistoryer); ok && m.active != nil {
		pairs = append(pairs, pair{"$", "cost chart"})
	}
	if m.active != nil {
		// `x` opens a themed PTY scoped to the active cloud — surface
		// it in the keybar so users discover it without the help screen.
		pairs = append(pairs, pair{"x", m.active.Name() + " term"})
	}
	if m.updateAvailable {
		// Bump the upgrade hint to the front of the keybar so it reads
		// as the first suggested action — the top-right pill already
		// announces the new release, this reinforces it.
		pairs = append([]pair{{"U", "upgrade now"}}, pairs...)
	} else {
		// Still advertise U so a user who wonders "am I on latest?"
		// has a discoverable way to trigger a fresh GitHub check.
		pairs = append(pairs, pair{"U", "check updates"})
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
		label := "0-5 filter"
		if m.categoryFilter != "" {
			label = "filter: " + m.categoryFilter
		}
		pairs = append(pairs, pair{"#", label})
		if n := len(m.selected); n > 0 {
			pairs = append(pairs, pair{"D", fmt.Sprintf("delete %d", n)})
		}
	}
	pairs = append(pairs,
		pair{"r", "refresh"},
		pair{"esc", "back"},
		pair{"q", "quit"},
	)
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, components.KeyPair(p.key, p.action))
	}
	return components.Keybar(m.width, parts)
}

func (m *model) atSubscriptionLevel() bool {
	if len(m.stack) == 0 {
		return false
	}
	top := &m.stack[len(m.stack)-1]
	return kindOf(top) == provider.KindSubscription
}

// applyChromeHeight sets the table height based on the current terminal
// height minus the surrounding chrome. We compute the header height by
// measuring the actual render with lipgloss.Height so changes to keybar
// wrapping stay in sync automatically.
func (m *model) applyChromeHeight() {
	if m.height <= 0 {
		return
	}
	header := m.headerView()
	chrome := lipgloss.Height(header)
	if m.categoryBar() != "" {
		chrome++
	}
	chrome++ // footer (always one line)
	h := m.height - chrome
	if h < 3 {
		h = 3
	}
	m.table.SetHeight(h)
	m.detail.Height = m.height - 3
	if m.detail.Height < 3 {
		m.detail.Height = 3
	}
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
	// Sticky deletion banner — wins over most footer content so the
	// user can see the confirmation even after the reload repopulates.
	// Errors and the active search input still take precedence.
	if banner := m.deletionBanner(); banner != "" && m.err == nil {
		return " " + banner
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
	// Truncation / "showing first N" should pop — otherwise the user easily
	// misses that they're looking at a partial view.
	if strings.HasPrefix(right, "showing first") {
		return " " + styles.WarnS.Render("⚠ "+right)
	}
	return " " + styles.Help.Render(right)
}

// deletionBanner renders the sticky confirmation after a delete. Empty
// when no banner is active or it's past its TTL. Pressing esc dismisses
// it via clearDeletionBanner.
func (m *model) deletionBanner() string {
	if m.pendingDelete == "" {
		return ""
	}
	if !m.pendingDeleteUntil.IsZero() && time.Now().After(m.pendingDeleteUntil) {
		return ""
	}
	return styles.Good.Bold(true).Render(m.pendingDelete) + "  " +
		styles.ModalHint.Render("(esc to dismiss)")
}

// loadingFooter renders the active-spinner footer line with the status in
// cyan + bold so it reads as "something is happening" instead of melting into
// the dim filter text.
func (m *model) loadingFooter(text string) string {
	return m.spinner.View() + " " + styles.Loading.Render(text)
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
