package cmd

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/tools"
)

//go:embed zellij/cloudnav.kdl
var zellijLayoutKDL string

//go:embed zellij/config.kdl
var zellijConfigKDL string

// envInsideZellij is the env var Zellij sets in every pane it spawns.
// Detecting it lets us refuse a recursive `cloudnav workspace` from
// inside an existing Zellij session instead of nesting multiplexers.
const envInsideZellij = "ZELLIJ"

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Launch cloudnav as a tab in a Zellij workspace",
	Long: `Run cloudnav inside a Zellij multiplexer session.

The session looks and feels like Zellij — Zellij's default theme,
its native tab bar / status bar / pane frames, its default
keybindings (Ctrl-p for pane mode, Ctrl-t for tab mode, etc.).
The TUI experience and the workspace experience stay deliberately
distinct so neither bleeds into the other.

Default layout: cloudnav fills its tab (the TUI was designed for
full width — the table, breadcrumb, and keybar all assume the
whole terminal is available). A sibling 'shell' tab is one
Ctrl-t '2' away for ad-hoc cloud-CLI commands (gcloud / aws /
az / kubectl / terraform / anything). If you want a shell pane
alongside the navigator, split with Ctrl-p '|' (vertical) or
Ctrl-p '-' (horizontal) — Zellij's native key.

The Zellij config + layout files are written to:

    <UserConfigDir>/cloudnav/zellij/

and selected via 'zellij --config-dir', so your own ~/.config/zellij
stays completely untouched. Standalone 'cloudnav' is unaffected.

Requires the 'zellij' binary in PATH. On macOS install with
'brew install zellij'; on Linux 'cargo install --locked zellij' or
your distro's package. Zellij isn't supported on Windows — use plain
'cloudnav' there.`,
	RunE: runWorkspace,
}

func runWorkspace(_ *cobra.Command, _ []string) error {
	if runtime.GOOS == "windows" {
		return errors.New("zellij is not supported on Windows — run 'cloudnav' directly")
	}
	if os.Getenv(envInsideZellij) != "" {
		return errors.New("already inside a Zellij session — just run 'cloudnav' (no workspace needed)")
	}
	zellijBin, err := exec.LookPath("zellij")
	if err != nil {
		// Auto-install when missing — matches the user's preference
		// for "if not found install it" rather than print hints and
		// bail. Falls back to a clear error when no package manager
		// is available (raw Linux without brew or cargo).
		fmt.Fprintln(os.Stderr, "→ zellij not found on PATH — installing automatically")
		if err := tools.Ensure(tools.Zellij, runtime.GOOS, os.Stderr); err != nil {
			return fmt.Errorf("zellij auto-install failed: %w", err)
		}
		zellijBin, err = exec.LookPath("zellij")
		if err != nil {
			return fmt.Errorf("zellij installed but not on PATH (you may need to "+
				"restart your shell so the new bin dir is picked up): %w", err)
		}
	}

	cfgDir, err := workspaceConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	if err := writeWorkspaceFiles(cfgDir); err != nil {
		return fmt.Errorf("write zellij files: %w", err)
	}

	layoutPath := filepath.Join(cfgDir, "cloudnav.kdl")
	c := exec.Command(zellijBin, "--config-dir", cfgDir, "--layout", layoutPath)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// workspaceConfigDir returns the isolated Zellij config directory
// cloudnav owns. Honours $CLOUDNAV_ZELLIJ_DIR (used by tests) before
// falling back to $XDG_CONFIG_HOME / os.UserConfigDir.
func workspaceConfigDir() (string, error) {
	if v := os.Getenv("CLOUDNAV_ZELLIJ_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cloudnav", "zellij"), nil
}

// writeWorkspaceFiles materialises the embedded layout + config under
// dir. Idempotent — overwrites every run so a cloudnav upgrade picks
// up theme tweaks automatically.
func writeWorkspaceFiles(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := []struct {
		name string
		data string
	}{
		{"cloudnav.kdl", zellijLayoutKDL},
		{"config.kdl", zellijConfigKDL},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.data), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(workspaceCmd)
}
