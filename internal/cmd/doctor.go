package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that required CLIs are installed and logged in",
	RunE: func(cmd *cobra.Command, _ []string) error {
		checks := []struct {
			name      string
			bin       string
			loginArgs []string
			installed string
		}{
			{"azure", "az", []string{"account", "show", "--query", "user.name", "-o", "tsv"}, "https://learn.microsoft.com/cli/azure/install-azure-cli"},
			{"gcp", "gcloud", []string{"auth", "list", "--filter=status:ACTIVE", "--format=value(account)"}, "https://cloud.google.com/sdk/docs/install"},
			{"aws", "aws", []string{"sts", "get-caller-identity", "--query", "Arn", "--output", "text"}, "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"},
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()

		missing := []string{}
		notAuthed := []string{}
		for _, c := range checks {
			path, err := exec.LookPath(c.bin)
			if err != nil {
				fmt.Printf("✗ %-6s %s not installed  · %s\n", c.name, c.bin, c.installed)
				missing = append(missing, c.name)
				continue
			}
			out, err := exec.CommandContext(ctx, c.bin, c.loginArgs...).CombinedOutput()
			if err != nil {
				fmt.Printf("✗ %-6s installed (%s) — not logged in → `cloudnav login %s`\n", c.name, path, c.name)
				notAuthed = append(notAuthed, c.name)
				continue
			}
			who := string(bytesTrim(out))
			if who == "" {
				who = "logged in"
			}
			fmt.Printf("✓ %-6s %s\n", c.name, firstLine(who))
		}
		if len(missing) == 0 && len(notAuthed) == 0 {
			return nil
		}
		fmt.Println()
		if len(missing) > 0 {
			fmt.Printf("next step — `cloudnav install <cloud>` for: %v\n", missing)
		}
		if len(notAuthed) > 0 {
			fmt.Printf("next step — run `cloudnav login <cloud>` for: %v\n", notAuthed)
		}
		return nil
	},
}

func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
