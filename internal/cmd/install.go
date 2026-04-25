package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
	"github.com/tesserix/cloudnav/internal/tools"
)

// toolZellij is the registered name of the Zellij TUI tool. Kept
// as a const so the install / upgrade ValidArgs and the
// dispatcher table reference exactly one source of truth — the
// goconst linter trips on three "zellij" string literals if we
// inline it.
const toolZellij = "zellij"

// toolByName looks up a third-party terminal tool registered in the
// internal/tools package. Add new tools by appending to this map and
// extending validInstallArgs / install help text.
func toolByName(name string) (tools.Tool, bool) {
	if name == toolZellij {
		return tools.Zellij, true
	}
	return tools.Tool{}, false
}

// validInstallArgs is the union of cloud names and registered TUI
// tool names that `cloudnav install` accepts.
var validInstallArgs = []string{cloudAzure, cloudAWS, cloudGCP, toolZellij}

var installCmd = &cobra.Command{
	Use:       "install [target]",
	Short:     "Install a cloud CLI (az / gcloud / aws) or TUI tool (zellij) using your OS's package manager",
	Long:      "Detects the current OS and runs the right install command — Homebrew on macOS (and Linux when available), the official curl installer on other Linux distros, or winget on Windows. After installing a cloud CLI, run 'cloudnav login <cloud>' to authenticate; credentials land in the CLI's standard location (~/.azure, ~/.config/gcloud, ~/.aws) which cloudnav reads transparently. TUI tools (currently 'zellij') install via brew or cargo depending on what's on PATH.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: validInstallArgs,
	RunE: func(_ *cobra.Command, args []string) error {
		// TUI tools take a different dispatch path than cloud
		// providers — they're not Provider implementations.
		if t, ok := toolByName(args[0]); ok {
			return tools.Run(t, "install", runtime.GOOS, os.Stderr)
		}
		return runCloudInstall(args[0])
	},
}

func runCloudInstall(name string) error {
	var p provider.Provider
	switch name {
	case cloudAzure:
		p = azure.New()
	case cloudAWS:
		p = aws.New()
	case cloudGCP:
		p = gcp.New()
	}
	inst, ok := p.(provider.Installer)
	if !ok {
		return fmt.Errorf("%s: no install recipe available", name)
	}
	steps, ok := inst.InstallPlan(runtime.GOOS)
	if !ok {
		if l, ok := p.(provider.Loginer); ok {
			return fmt.Errorf("no automated installer for %s on %s — %s", name, runtime.GOOS, l.InstallHint())
		}
		return fmt.Errorf("no automated installer for %s on %s", name, runtime.GOOS)
	}
	fmt.Fprintf(os.Stderr, "→ installing %s CLI via:\n", name)
	for _, s := range steps {
		fmt.Fprintf(os.Stderr, "    %s\n", s.Description)
	}
	fmt.Fprintln(os.Stderr)
	for _, s := range steps {
		if _, err := exec.LookPath(s.Bin); err != nil {
			return fmt.Errorf("required tool %q not found in PATH — can't run: %s", s.Bin, s.Description)
		}
		c := exec.Command(s.Bin, s.Args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %w", s.Description, err)
		}
	}
	fmt.Fprintf(os.Stderr, "\n✓ %s CLI installed. Next: cloudnav login %s\n", name, name)
	return nil
}

// upgradeCmd handles explicit upgrades of TUI tools cloudnav manages
// (currently zellij). Cloud-CLI upgrades stay with each provider's
// own self-update path (e.g. `gcloud components update`).
var upgradeCmd = &cobra.Command{
	Use:       "upgrade [tool]",
	Short:     "Upgrade a TUI tool (zellij) using your OS's package manager",
	Long:      "Runs the right upgrade command for the current OS — 'brew update && brew upgrade <tool>' on Homebrew, 'cargo install --locked --force <tool>' on Cargo. Idempotent — safe to run when the tool is already at the latest version.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{toolZellij},
	RunE: func(_ *cobra.Command, args []string) error {
		t, ok := toolByName(args[0])
		if !ok {
			return fmt.Errorf("%s: no upgrade recipe — only zellij is managed by 'cloudnav upgrade' today", args[0])
		}
		return tools.Run(t, "upgrade", runtime.GOOS, os.Stderr)
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(upgradeCmd)
}
