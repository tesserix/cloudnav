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
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
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
	nodesLoadedMsg struct{ frame frame }
	errMsg         struct{ err error }
)

type model struct {
	ctx       context.Context
	providers []provider.Provider
	active    provider.Provider
	stack     []frame
	table     table.Model
	spinner   spinner.Model
	loading   bool
	err       error
	status    string
	showHelp  bool
	width     int
	height    int
	keys      keys.Map
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

	m := &model{
		ctx:       context.Background(),
		providers: []provider.Provider{azure.New()},
		spinner:   sp,
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
	home.nodes = append(home.nodes,
		provider.Node{Name: "gcp (coming soon)", Kind: provider.KindCloudDisabled},
		provider.Node{Name: "aws (coming soon)", Kind: provider.KindCloudDisabled},
	)
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
		}
		if h := msg.Height - 4; h > 0 {
			m.table.SetHeight(h)
		}
		m.refreshTable()
		return m, nil

	case tea.KeyMsg:
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
		m.status = fmt.Sprintf("%d items", len(msg.frame.nodes))
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

func (m *model) drillDown() tea.Cmd {
	top := m.stack[len(m.stack)-1]
	if len(top.nodes) == 0 {
		return nil
	}
	cur := top.nodes[m.table.Cursor()]
	switch cur.Kind {
	case provider.KindCloud:
		for _, p := range m.providers {
			if p.Name() == cur.Name {
				m.active = p
				return m.load(p.Name(), nil)
			}
		}
	case provider.KindCloudDisabled:
		m.status = "coming soon"
	case provider.KindSubscription:
		return m.load(cur.Name, &cur)
	case provider.KindResourceGroup:
		return m.load(cur.Name, &cur)
	}
	return nil
}

func (m *model) goBack() tea.Cmd {
	if len(m.stack) <= 1 {
		return tea.Quit
	}
	m.stack = m.stack[:len(m.stack)-1]
	if len(m.stack) == 1 {
		m.active = nil
	}
	m.refreshTable()
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

func (m *model) openPortal() {
	if m.active == nil || len(m.stack) <= 1 {
		return
	}
	top := m.stack[len(m.stack)-1]
	if len(top.nodes) == 0 {
		return
	}
	cur := top.nodes[m.table.Cursor()]
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
	m.table.SetColumns(columnsFor(top))
	m.table.SetRows(rowsFor(top))
}

func columnsFor(f *frame) []table.Column {
	if f.title == "clouds" {
		return []table.Column{{Title: "CLOUD", Width: 40}}
	}
	return []table.Column{
		{Title: "NAME", Width: 44},
		{Title: "LOCATION", Width: 16},
		{Title: "STATE", Width: 20},
		{Title: "ID", Width: 50},
	}
}

func rowsFor(f *frame) []table.Row {
	rows := make([]table.Row, 0, len(f.nodes))
	for _, n := range f.nodes {
		if f.title == "clouds" {
			rows = append(rows, table.Row{n.Name})
			continue
		}
		rows = append(rows, table.Row{n.Name, n.Location, n.State, shorten(n.ID, 50)})
	}
	return rows
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

func (m *model) View() string {
	if m.showHelp {
		return m.helpView()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.table.View(),
		m.footerView(),
	)
}

func (m *model) headerView() string {
	title := styles.Title.Render("cloudnav")
	crumbs := strings.Join(breadcrumbs(m.stack), styles.CrumbSep)
	left := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", styles.Crumb.Render(crumbs))
	right := styles.Help.Render(currentProvider(m))
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
	if m.loading {
		return styles.StatusBar.Render(m.spinner.View() + " " + m.status)
	}
	if m.err != nil {
		return styles.StatusBar.Render(styles.Bad.Render("error: " + m.err.Error()))
	}
	hints := []string{
		styles.Key.Render("↵") + " open",
		styles.Key.Render("esc") + " back",
		styles.Key.Render("/") + " search",
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
		"s              cycle sort",
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
