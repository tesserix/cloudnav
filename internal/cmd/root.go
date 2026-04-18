// Package cmd wires the Cobra commands that make up the cloudnav CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/tui"
)

var rootCmd = &cobra.Command{
	Use:   "cloudnav",
	Short: "Multi-cloud TUI for Azure, GCP, and AWS",
	Long: `cloudnav is a fast, keyboard-driven TUI that drills through tenants,
subscriptions, projects, accounts, resources, costs, and IAM across Azure,
GCP, and AWS — using the credentials your cloud CLIs are already logged in
with.

First time?  Run 'cloudnav doctor' to see which clouds need attention.
If a cloud isn't logged in yet, either:
  cloudnav login azure        # wraps 'az login'
  cloudnav login gcp          # wraps 'gcloud auth login'
  cloudnav login aws          # wraps 'aws sso login'
or open the TUI and press 'I' on the cloud row.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
			fmt.Fprintln(os.Stderr, "cloudnav: the TUI requires an attached terminal.")
			fmt.Fprintln(os.Stderr, "For non-interactive use, try:")
			fmt.Fprintln(os.Stderr, "  cloudnav doctor")
			fmt.Fprintln(os.Stderr, "  cloudnav ls azure subs --json")
			fmt.Fprintln(os.Stderr, "  cloudnav --help")
			return fmt.Errorf("no TTY available")
		}
		return tui.Run()
	},
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd, versionCmd, lsCmd, completionCmd)
}
