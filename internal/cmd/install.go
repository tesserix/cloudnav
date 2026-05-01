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

// validInstallArgs is the set of cloud names `cloudnav install` accepts.
var validInstallArgs = []string{cloudAzure, cloudAWS, cloudGCP}

var installCmd = &cobra.Command{
	Use:       "install [cloud]",
	Short:     "Install a cloud CLI (az / gcloud / aws) using your OS's package manager",
	Long:      "Detects the current OS and runs the right install command — Homebrew on macOS (and Linux when available), the official curl installer on other Linux distros, or winget on Windows. After installing a cloud CLI, run 'cloudnav login <cloud>' to authenticate; credentials land in the CLI's standard location (~/.azure, ~/.config/gcloud, ~/.aws) which cloudnav reads transparently.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: validInstallArgs,
	RunE: func(_ *cobra.Command, args []string) error {
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

func init() {
	rootCmd.AddCommand(installCmd)
}
