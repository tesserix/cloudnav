package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
)

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

// execShell opens the embedded PTY terminal page, themed to the
// active cloud, with the row's context (subscription / project /
// account / resource group) exported as env vars. Replaces the old
// "suspend the TUI and exec a real shell" flow — keeps the user
// inside cloudnav's chrome and brand colours.
func (m *model) execShell() tea.Cmd {
	if m.active == nil {
		m.status = "drill into a cloud first — `x` opens a terminal scoped to the active cloud"
		return nil
	}
	c := m.table.Cursor()
	var cur provider.Node
	if c >= 0 && c < len(m.visibleNodes) {
		cur = m.visibleNodes[c]
	}
	cloud := m.active.Name()
	context, env := buildTermContext(cloud, cur)
	if context == "" {
		// At the cloud level, with nothing under the cursor, we still
		// open a terminal — but flag in the chrome that no per-row
		// context is set so users know to drill in for a scoped one.
		context = "no row context — drill into a sub / project / account for env vars"
	}
	if m.width <= 0 || m.height <= 0 {
		m.status = "terminal needs a sized window — try resizing"
		return nil
	}
	t, cmd, err := startTerminal(cloud, context, env, m.width, m.height)
	if err != nil {
		m.status = "open terminal failed: " + err.Error()
		return nil
	}
	m.term = t
	return cmd
}
