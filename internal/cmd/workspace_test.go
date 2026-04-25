package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceConfigDirHonoursOverride(t *testing.T) {
	t.Setenv("CLOUDNAV_ZELLIJ_DIR", "/tmp/cloudnav-zellij-test")
	got, err := workspaceConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/cloudnav-zellij-test" {
		t.Errorf("workspaceConfigDir = %q, want override", got)
	}
}

func TestWorkspaceConfigDirDefaultPath(t *testing.T) {
	t.Setenv("CLOUDNAV_ZELLIJ_DIR", "")
	got, err := workspaceConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	// We don't assert the absolute path (varies by OS), only that it
	// terminates with cloudnav/zellij so we don't accidentally write
	// into the user's ~/.config/zellij.
	want := filepath.Join("cloudnav", "zellij")
	if !strings.HasSuffix(got, want) {
		t.Errorf("workspaceConfigDir = %q, want suffix %q (so we never clobber the user's own zellij config)", got, want)
	}
}

func TestWriteWorkspaceFilesMaterialisesLayoutAndConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeWorkspaceFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cloudnav.kdl", "config.kdl"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestWriteWorkspaceFilesIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := writeWorkspaceFiles(dir); err != nil {
		t.Fatal(err)
	}
	// Running again must not error and must overwrite to refresh
	// after a cloudnav upgrade.
	if err := writeWorkspaceFiles(dir); err != nil {
		t.Errorf("second write should succeed: %v", err)
	}
}

func TestEmbeddedLayoutPointsAtCloudnav(t *testing.T) {
	if !strings.Contains(zellijLayoutKDL, `command "cloudnav"`) {
		t.Error("layout KDL should run the cloudnav binary in one of the panes")
	}
	if !strings.Contains(zellijLayoutKDL, "tab name=\"cloudnav\"") {
		t.Error("layout KDL should name the navigator tab 'cloudnav'")
	}
}

// TestEmbeddedLayoutKeepsZellijNative pins the design choice that
// `cloudnav workspace` looks like Zellij — Zellij's default
// theme, default tab/status bars, default chrome — with cloudnav
// as one of the panes inside. Earlier iterations themed Zellij
// to mimic the cloudnav TUI; user feedback was that the two
// experiences should stay visually distinct.
func TestEmbeddedLayoutKeepsZellijNative(t *testing.T) {
	if strings.Contains(zellijConfigKDL, `theme "cloudnav"`) {
		t.Error("config KDL must NOT force a custom theme — Zellij should look like Zellij")
	}
	if strings.Contains(zellijConfigKDL, `themes {`) {
		t.Error("config KDL must NOT declare a themes block — let Zellij use its default")
	}
	// Multi-pane: a real Zellij workspace, not a re-skin of the
	// TUI as the entire session.
	if !strings.Contains(zellijLayoutKDL, `split_direction="vertical"`) {
		t.Error("layout KDL should use a vertical split so cloudnav and a shell live side-by-side")
	}
	// Standard Zellij plugins for tab + status bars must be
	// present so users keep Zellij's native discoverability.
	if !strings.Contains(zellijLayoutKDL, `plugin location="zellij:tab-bar"`) {
		t.Error("layout KDL should keep Zellij's native tab bar")
	}
	if !strings.Contains(zellijLayoutKDL, `plugin location="zellij:status-bar"`) {
		t.Error("layout KDL should keep Zellij's native status bar (mode + keybinding hints)")
	}
}
