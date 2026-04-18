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
)

var installCmd = &cobra.Command{
	Use:       "install [cloud]",
	Short:     "Install a cloud CLI (az / gcloud / aws) using your OS's package manager",
	Long:      "Detects the current OS and runs the right install command — Homebrew on macOS (and Linux when available), the official curl installer on other Linux distros, or winget on Windows. After install finishes, run 'cloudnav login <cloud>' to authenticate; credentials land in the CLI's standard location (~/.azure, ~/.config/gcloud, ~/.aws) which cloudnav reads transparently.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"azure", "aws", "gcp"},
	RunE: func(cmd *cobra.Command, args []string) error {
		var p provider.Provider
		switch args[0] {
		case "azure":
			p = azure.New()
		case "aws":
			p = aws.New()
		case "gcp":
			p = gcp.New()
		}
		inst, ok := p.(provider.Installer)
		if !ok {
			return fmt.Errorf("%s: no install recipe available", args[0])
		}
		steps, ok := inst.InstallPlan(runtime.GOOS)
		if !ok {
			if l, ok := p.(provider.Loginer); ok {
				return fmt.Errorf("no automated installer for %s on %s — %s", args[0], runtime.GOOS, l.InstallHint())
			}
			return fmt.Errorf("no automated installer for %s on %s", args[0], runtime.GOOS)
		}
		fmt.Fprintf(os.Stderr, "→ installing %s CLI via:\n", args[0])
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
		fmt.Fprintf(os.Stderr, "\n✓ %s CLI installed. Next: cloudnav login %s\n", args[0], args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
