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

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
	"github.com/tesserix/cloudnav/internal/tui/keys"
	"github.com/tesserix/cloudnav/internal/tui/styles"
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

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle()

	m := &model{
		ctx:       context.Background(),
		providers: []provider.Provider{azure.New(), gcp.New(), aws.New()},
		spinner:   sp,
		search:    ti,
		detail:    vp,
		keys:      keys.Default(),
		table:     t,
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
		if m.detailMode {
			if msg.String() == "esc" || msg.String() == "q" {
				m.detailMode = false
				return m, nil
			}
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
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
		case key.Matches(msg, m.keys.Sort):
			m.sort = (m.sort + 1) % 3
			m.refreshTable()
			m.status = "sort: " + m.sort.String()
			return m, nil
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

func (m *model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.search.Blur()
		m.filter = ""
		m.search.SetValue("")
		m.refreshTable()
		return m, nil
	case "enter":
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
	pimer, ok := m.active.(provider.PIMer)
	if !ok {
		m.status = m.active.Name() + ": PIM not supported"
		return nil
	}
	m.loading = true
	m.status = "loading PIM eligible roles..."
	ctx := m.ctx
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			roles, err := pimer.ListEligibleRoles(ctx)
			if err != nil {
				return errMsg{err}
			}
			return detailLoadedMsg{title: "PIM eligible roles", body: renderPIM(roles)}
		},
	)
}

func renderPIM(roles []provider.PIMRole) string {
	if len(roles) == 0 {
		return "No PIM-eligible roles found for your user.\n\nThis means either:\n  • you have no eligible PIM assignments, or\n  • your tenant does not use PIM, or\n  • you do not have read access to roleEligibilityScheduleInstances"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d eligible role assignment(s)\n\n", len(roles))
	for i, r := range roles {
		fmt.Fprintf(&b, "%d) %s\n", i+1, r.RoleName)
		if r.ScopeName != "" {
			fmt.Fprintf(&b, "   on:     %s\n", r.ScopeName)
			fmt.Fprintf(&b, "   scope:  %s\n", r.Scope)
		} else {
			fmt.Fprintf(&b, "   scope:  %s\n", r.Scope)
		}
		if r.EndDateTime != "" {
			fmt.Fprintf(&b, "   until:  %s\n", r.EndDateTime)
		}
		fmt.Fprintln(&b)
	}
	b.WriteString("(activation via keybinding — coming in a follow-up commit)")
	return b.String()
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
	// Clear rows first — bubbles/table.SetColumns re-renders existing rows,
	// and if the new columns have fewer cells than the old rows, renderRow
	// indexes past the column slice and panics.
	m.table.SetRows(nil)
	m.table.SetColumns(columnsFor(top))
	m.table.SetRows(rowsFromNodes(top.title, m.visibleNodes))
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
		sort.SliceStable(out, func(i, j int) bool {
			ai := out[i].Kind == provider.KindCloudDisabled
			aj := out[j].Kind == provider.KindCloudDisabled
			if ai != aj {
				return !ai
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
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

func columnsFor(f *frame) []table.Column {
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
		return []table.Column{
			{Title: "NAME", Width: 44},
			{Title: "PROJECT ID", Width: 30},
			{Title: "STATE", Width: 16},
		}
	case provider.KindAccount:
		return []table.Column{
			{Title: "ACCOUNT", Width: 18},
			{Title: "ARN", Width: 60},
			{Title: "STATE", Width: 12},
		}
	case provider.KindRegion:
		return []table.Column{
			{Title: "REGION", Width: 24},
			{Title: "ENDPOINT", Width: 42},
			{Title: "STATE", Width: 14},
		}
	case provider.KindResourceGroup:
		return []table.Column{
			{Title: "NAME", Width: 56},
			{Title: "LOCATION", Width: 20},
			{Title: "STATE", Width: 18},
		}
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

func rowsFromNodes(_ string, nodes []provider.Node) []table.Row {
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
			rows = append(rows, table.Row{n.Name, n.ID, n.State})
		case provider.KindAccount:
			rows = append(rows, table.Row{n.Name, shorten(n.Meta["arn"], 60), n.State})
		case provider.KindRegion:
			rows = append(rows, table.Row{n.Name, shorten(n.Meta["endpoint"], 42), n.State})
		case provider.KindResourceGroup:
			rows = append(rows, table.Row{n.Name, n.Location, n.State})
		case provider.KindResource:
			rows = append(rows, table.Row{n.Name, n.Meta["type"], n.Location})
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
		"/              search current view",
		":              command palette",
		"c              cost column toggle",
		"s              cycle sort (name / state / location)",
		"o              open selected in cloud portal",
		"i              json detail",
		"p              PIM eligible roles (Azure)",
		"x              exec provider CLI in current scope",
		"r              refresh",
		"f              bookmark current view",
		"?              help",
		"q / ctrl+c     quit",
		"",
		styles.Help.Render("press any key to close"),
	}, "\n")
	return styles.Box.Render(body)
}
