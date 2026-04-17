package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/azure"
)

var pimCmd = &cobra.Command{
	Use:   "pim",
	Short: "Privileged Identity Management (Azure)",
}

var pimListCmd = &cobra.Command{
	Use:   "list",
	Short: "List PIM-eligible role assignments for the current user",
	RunE: func(cmd *cobra.Command, _ []string) error {
		pimer := azure.New()
		if _, err := pimer.Root(cmd.Context()); err != nil {
			return err
		}
		roles, err := pimer.ListEligibleRoles(cmd.Context())
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(roles)
		}
		for i, r := range roles {
			scope := r.ScopeName
			if scope == "" {
				scope = r.Scope
			}
			fmt.Printf("%2d. %-40s %s\n", i+1, r.RoleName, scope)
		}
		return nil
	},
}

var pimActivateCmd = &cobra.Command{
	Use:   "activate <index>",
	Short: "Activate a PIM-eligible role by 1-based index from `pim list`",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reason, _ := cmd.Flags().GetString("reason")
		duration, _ := cmd.Flags().GetInt("duration")
		if reason == "" {
			return fmt.Errorf("--reason is required by PIM policy")
		}
		idx, err := parseIndex(args[0])
		if err != nil {
			return err
		}

		ctx := context.Background()
		p := azure.New()
		if _, err := p.Root(ctx); err != nil {
			return err
		}
		roles, err := p.ListEligibleRoles(ctx)
		if err != nil {
			return err
		}
		if idx < 1 || idx > len(roles) {
			return fmt.Errorf("index %d out of range (1..%d)", idx, len(roles))
		}
		role := roles[idx-1]

		fmt.Printf("activating %q on %s for %dh — reason: %q\n",
			role.RoleName, scopeOrName(role), duration, reason)
		if err := p.ActivateRole(ctx, role, reason, duration); err != nil {
			return err
		}
		fmt.Println("✓ activation request submitted")
		return nil
	},
}

func scopeOrName(r provider.PIMRole) string {
	if r.ScopeName != "" {
		return r.ScopeName
	}
	return r.Scope
}

func parseIndex(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	if err != nil {
		return 0, fmt.Errorf("bad index %q: %w", s, err)
	}
	return i, nil
}

func init() {
	pimCmd.AddCommand(pimListCmd, pimActivateCmd)
	pimListCmd.Flags().Bool("json", false, "Emit JSON")
	pimActivateCmd.Flags().String("reason", "", "Justification sent to PIM (required)")
	pimActivateCmd.Flags().Int("duration", 1, "Activation duration in hours (capped by role policy)")
	rootCmd.AddCommand(pimCmd)
}
