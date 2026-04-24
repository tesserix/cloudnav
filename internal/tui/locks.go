package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
)

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
