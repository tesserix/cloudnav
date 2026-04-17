// Package cmd wires the Cobra commands that make up the cloudnav CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/tui"
)

var rootCmd = &cobra.Command{
	Use:           "cloudnav",
	Short:         "Multi-cloud TUI for Azure, GCP, and AWS",
	Long:          "cloudnav is a fast, keyboard-driven TUI that drills through tenants, subscriptions, projects, accounts, resources, costs, and IAM across Azure, GCP, and AWS — using the credentials your cloud CLIs are already logged in with.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
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
