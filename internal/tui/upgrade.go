package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/tui/styles"
	"github.com/tesserix/cloudnav/internal/updatecheck"
	"github.com/tesserix/cloudnav/internal/version"
)

// loadUpdateCheck kicks off a background poll of the GitHub releases
// API. The check is wrapped in its own short-lived context so a flaky
// network can't keep the TUI from starting. Failures are silent —
// updateCheckMsg carries an empty Latest and the header falls back to
// the quiet state.
func (m *model) loadUpdateCheck() tea.Cmd {
	parent := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 6*time.Second)
		defer cancel()
		return updateCheckMsg{result: updatecheck.Check(ctx, version.Version)}
	}
}

// openUpgrade opens the upgrade confirmation overlay. When no newer
// release has been detected the key kicks off a fresh GitHub check
// (rather than sitting behind a potentially-stale result) and updates
// the status line so the user knows we looked, even if the answer is
// "already on the latest tag".
func (m *model) openUpgrade() tea.Cmd {
	if !m.updateAvailable {
		m.status = "checking GitHub for a newer release..."
		return m.loadUpdateCheck()
	}
	m.upgradePlan = updatecheck.PlanUpgrade(m.latestVersion, m.latestURL)
	m.upgradeResult = ""
	m.upgradeErr = nil
	m.upgradeRunning = false
	m.upgradeMode = true
	return nil
}

// updateUpgrade handles keys inside the upgrade confirmation overlay.
// y / enter runs the detected plan; esc / n dismisses; r relaunches
// the freshly-installed binary in place so the user's session moves to
// the new version without them retyping the command.
func (m *model) updateUpgrade(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.upgradeRunning {
		// Block keystrokes while the subprocess is running — otherwise a
		// stray Enter could fire a second `go install` on top of the
		// first. Esc is intentionally ignored too so the user doesn't
		// dismiss the overlay before the command returns.
		return m, nil
	}
	// After a successful upgrade the 'R' key is the primary path: flip
	// relaunch, then quit. Run() execs the new binary after bubbletea
	// tears down.
	if m.upgradeResult != "" && m.upgradeErr == nil {
		switch msg.String() {
		case "r", "R", keyEnter:
			m.relaunch = true
			return m, tea.Quit
		case keyEsc, "q", "n", "N":
			m.upgradeMode = false
			return m, nil
		}
		return m, nil
	}
	switch msg.String() {
	case keyEsc, "q", "n", "N":
		m.upgradeMode = false
		m.upgradeResult = ""
		m.upgradeErr = nil
		return m, nil
	case "y", "Y", keyEnter:
		return m, func() tea.Msg { return upgradeStartMsg{} }
	}
	return m, nil
}

// runUpgrade executes the resolved UpgradePlan. Runs on the tea
// command goroutine so Bubbletea redraws (spinner / status) keep
// ticking while we wait on go install / brew / the browser handoff.
func (m *model) runUpgrade() tea.Cmd {
	plan := m.upgradePlan
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		out, err := updatecheck.Run(ctx, plan)
		return upgradeResultMsg{summary: out, err: err}
	}
}

// upgradeView renders the confirmation / progress overlay.
func (m *model) upgradeView() string {
	plan := m.upgradePlan
	title := styles.Title.Render("cloudnav upgrade")
	current := styles.Help.Render("current: " + version.Version)
	target := styles.AccentS.Render("available: " + m.latestVersion)
	why := styles.Help.Render("  " + plan.Why)

	var action string
	switch plan.Method {
	case updatecheck.UpgradeGoInstall, updatecheck.UpgradeHomebrew:
		action = styles.Key.Render("  $ "+plan.Bin+" "+strings.Join(plan.Args, " ")) + "\n" + why
	case updatecheck.UpgradeManual:
		action = styles.Key.Render("  open release page") + "\n" + styles.Help.Render("  "+plan.URL)
	}

	var footer string
	switch {
	case m.upgradeRunning:
		footer = styles.WarnS.Render("  running... " + m.spinner.View())
	case m.upgradeErr != nil:
		footer = styles.Bad.Render("  ✗ "+firstErrLine(m.upgradeErr)) +
			"\n" + styles.Help.Render("  press esc to close")
		if m.upgradeResult != "" {
			footer += "\n" + styles.Help.Render("  "+m.upgradeResult)
		}
	case m.upgradeResult != "":
		footer = styles.Good.Bold(true).Render("  ✓ upgrade complete — running v"+strings.TrimPrefix(m.latestVersion, "v")) +
			"\n\n" + styles.Key.Render("  press R or ↵ to relaunch now   ·   esc to stay on the current version") +
			"\n" + styles.Help.Render("  "+m.upgradeResult)
	default:
		footer = styles.Help.Render("  y / ↵ run   ·   n / esc cancel")
	}

	body := strings.Join([]string{
		title,
		"",
		"  " + current,
		"  " + target,
		"",
		action,
		"",
		footer,
	}, "\n")
	return m.overlay(body)
}
