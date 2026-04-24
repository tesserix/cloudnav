package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/tui/styles"
)

// deleteScope is what level the confirmation overlay is acting on — so
// the heading and disclaimer match what's actually about to happen.
type deleteScope int

const (
	deleteScopeRG deleteScope = iota
	deleteScopeResource
)

// deleteFailure preserves per-target failure detail so the status bar
// and detail overlay can surface the real Azure error, not just a count.
type deleteFailure struct {
	Name string
	Err  error
}

func (m *model) promptDelete() {
	switch {
	case m.atRGLevel():
		m.promptDeleteRGs()
	case m.atResourceLevel():
		m.promptDeleteResources()
	default:
		m.status = "D works on the resource-groups or resources view"
	}
}

func (m *model) promptDeleteRGs() {
	if _, ok := m.active.(*azure.Azure); !ok {
		m.status = "delete is Azure-only"
		return
	}
	targets := m.selectedOrCursor()
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
	m.enterDeleteMode(targets, deleteScopeRG)
}

func (m *model) promptDeleteResources() {
	if _, ok := m.active.(*azure.Azure); !ok {
		m.status = "delete is Azure-only"
		return
	}
	targets := m.selectedOrCursor()
	if len(targets) == 0 {
		m.status = "nothing selected — use space to select rows, [ to select all, D to delete"
		return
	}
	m.enterDeleteMode(targets, deleteScopeResource)
}

// selectedOrCursor returns every visible row marked via space-select; if
// nothing is selected it falls back to the row under the cursor so the
// D key still does something on a single row without needing a select.
func (m *model) selectedOrCursor() []provider.Node {
	out := make([]provider.Node, 0, len(m.selected))
	for _, n := range m.visibleNodes {
		if m.selected[n.ID] {
			out = append(out, n)
		}
	}
	if len(out) > 0 {
		return out
	}
	if c := m.table.Cursor(); c >= 0 && c < len(m.visibleNodes) {
		return []provider.Node{m.visibleNodes[c]}
	}
	return nil
}

func (m *model) enterDeleteMode(targets []provider.Node, scope deleteScope) {
	m.deleteMode = true
	m.deleteTargets = targets
	m.deleteScope = scope
	m.deleteInput.SetValue("")
	m.deleteInput.Focus()
	m.status = ""
}

// executeDelete fires the actual async deletion after the user has typed
// the confirmation word. Runs requests in parallel (bounded by an 8-way
// semaphore so we don't hammer ARM) and collects every failure so the
// user sees the actual Azure error string instead of a counted summary.
func (m *model) executeDelete() tea.Cmd {
	az, ok := m.active.(*azure.Azure)
	if !ok || len(m.deleteTargets) == 0 {
		m.deleteMode = false
		return nil
	}
	targets := m.deleteTargets
	scope := m.deleteScope
	subID := m.currentSubID()
	ctx := m.ctx
	m.deleteMode = false
	m.deleteInput.Blur()
	m.deleteTargets = nil
	m.selected = map[string]bool{}
	m.loading = true
	if scope == deleteScopeResource {
		m.status = fmt.Sprintf("deleting %d resource(s)...", len(targets))
	} else {
		m.status = fmt.Sprintf("deleting %d resource group(s) asynchronously...", len(targets))
	}
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			failures := runDeletes(ctx, az, subID, targets, scope)
			if len(failures) == 0 {
				return deletedMsg{
					msg: fmt.Sprintf("deletion requested for %d %s — Azure is processing",
						len(targets), deleteNoun(scope, len(targets))),
				}
			}
			return deletedMsg{
				msg: fmt.Sprintf("%d of %d deletions failed", len(failures), len(targets)),
				err: failuresToErr(failures),
			}
		},
	)
}

// runDeletes fans out to up to 8 concurrent requests so deleting a batch
// of 50 resources doesn't serialise for the user. Each failure is
// captured with the target name so the caller can render them with
// context instead of a generic count.
func runDeletes(ctx context.Context, az *azure.Azure, subID string, targets []provider.Node, scope deleteScope) []deleteFailure {
	sem := make(chan struct{}, 8)
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []deleteFailure
	)
	for _, t := range targets {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var err error
			switch scope {
			case deleteScopeResource:
				err = az.DeleteResource(ctx, targetSubID(t, subID), t.ID, t.Meta["type"])
			default:
				err = az.DeleteResourceGroup(ctx, targetSubID(t, subID), t.Name)
			}
			if err != nil {
				mu.Lock()
				failed = append(failed, deleteFailure{Name: t.Name, Err: err})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return failed
}

// targetSubID picks the subscription id for a given node. Resource nodes
// sometimes carry subscriptionId in Meta (from Resource Graph); RG rows
// inherit the caller's active subscription.
func targetSubID(n provider.Node, fallback string) string {
	if id := n.Meta["subscriptionId"]; id != "" {
		return id
	}
	return fallback
}

// failuresToErr joins per-target errors into one multi-line error so the
// detail overlay can render them all, and the status bar gets a short
// first line via firstErrLine().
func failuresToErr(fails []deleteFailure) error {
	parts := make([]string, 0, len(fails))
	for _, f := range fails {
		parts = append(parts, fmt.Sprintf("%s: %s", f.Name, firstErrLine(f.Err)))
	}
	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}

func deleteNoun(scope deleteScope, n int) string {
	if scope == deleteScopeResource {
		if n == 1 {
			return "resource"
		}
		return "resources"
	}
	if n == 1 {
		return "resource group"
	}
	return "resource groups"
}

// updateDeleteConfirm handles keys inside the delete confirmation overlay.
// Enter with "DELETE" typed fires; anything else (including esc or Enter
// with a wrong word) cancels. Matching is case-insensitive for user
// comfort but an empty input never proceeds — that's the safety floor.
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

// deleteConfirmView renders the destructive-action modal. Heading and
// disclaimer change depending on whether we're deleting resource groups
// (Azure async tear-down) or individual resources (single-shot DELETE).
func (m *model) deleteConfirmView() string {
	var (
		heading    string
		disclaimer []string
	)
	switch m.deleteScope {
	case deleteScopeResource:
		heading = "⚠  DELETE RESOURCES"
		disclaimer = []string{
			"Each resource is removed via a direct ARM DELETE.",
			"Depending on the type, the tear-down can be immediate or",
			"take a few minutes. The operation cannot be undone.",
		}
	default:
		heading = "⚠  DELETE RESOURCE GROUPS"
		disclaimer = []string{
			"Everything inside each resource group — VMs, databases,",
			"storage, keys, backups — goes with it. The operation",
			"cannot be undone.",
		}
	}

	lines := make([]string, 0, 3+len(m.deleteTargets)+1+len(disclaimer)+4)
	lines = append(lines,
		styles.Bad.Render(heading),
		"",
		styles.Header.Render("This will permanently delete:"),
	)
	for i, t := range m.deleteTargets {
		right := t.Location
		if m.deleteScope == deleteScopeResource && t.Meta["type"] != "" {
			right = t.Meta["type"]
		}
		lines = append(lines, fmt.Sprintf("  %2d. %s   %s", i+1, t.Name, styles.Help.Render(right)))
	}
	lines = append(lines, "")
	for _, line := range disclaimer {
		lines = append(lines, styles.Bad.Render(line))
	}
	lines = append(lines,
		"",
		m.deleteInput.View(),
		"",
		styles.ModalHint.Render("enter  proceed     esc  cancel"),
	)
	return m.overlay(strings.Join(lines, "\n"))
}
