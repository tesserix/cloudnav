package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
)

var pimCmd = &cobra.Command{
	Use:   "pim",
	Short: "Privileged Identity Management / JIT elevation",
	Long:  "Azure: Privileged Identity Management across Azure resource RBAC, Entra directory roles, and PIM for Groups.\nAWS:   AWS SSO profile sign-in via browser.\nGCP:   Privileged Access Manager (PAM) entitlements — falls back to a conditional-IAM template when PAM isn't enabled.",
}

var pimListCmd = &cobra.Command{
	Use:   "list",
	Short: "List PIM-eligible / SSO / JIT roles for the current user",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		asJSON, _ := cmd.Flags().GetBool("json")

		p, err := pickProvider(cloud)
		if err != nil {
			return err
		}
		pimer, ok := p.(provider.PIMer)
		if !ok {
			return fmt.Errorf("%s: JIT/PIM not supported", cloud)
		}
		if _, err := p.Root(cmd.Context()); err != nil && cloud == cloudAzure {
			return err
		}
		roles, err := pimer.ListEligibleRoles(cmd.Context())
		if err != nil {
			return err
		}
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
	Short: "Activate role #N from `pim list`",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		reason, _ := cmd.Flags().GetString("reason")
		duration, _ := cmd.Flags().GetInt("duration")
		idx, err := parseIndex(args[0])
		if err != nil {
			return err
		}

		ctx := context.Background()
		p, err := pickProvider(cloud)
		if err != nil {
			return err
		}
		pimer, ok := p.(provider.PIMer)
		if !ok {
			return fmt.Errorf("%s: JIT/PIM not supported", cloud)
		}
		if _, err := p.Root(ctx); err != nil && cloud == cloudAzure {
			return err
		}
		if cloud == cloudAzure && reason == "" {
			return fmt.Errorf("--reason is required for azure PIM activation")
		}

		roles, err := pimer.ListEligibleRoles(ctx)
		if err != nil {
			return err
		}
		if idx < 1 || idx > len(roles) {
			return fmt.Errorf("index %d out of range (1..%d)", idx, len(roles))
		}
		role := roles[idx-1]

		fmt.Printf("activating %q on %s\n", role.RoleName, scopeOrName(role))
		if err := pimer.ActivateRole(ctx, role, reason, duration); err != nil {
			return err
		}
		fmt.Println("✓ activation submitted")
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
	pimCmd.PersistentFlags().String("cloud", "azure", "cloud to target: azure | aws | gcp")
	pimListCmd.Flags().Bool("json", false, "Emit JSON")
	pimActivateCmd.Flags().String("reason", "", "Justification (required for Azure)")
	pimActivateCmd.Flags().Int("duration", 1, "Activation duration in hours (Azure only)")
	rootCmd.AddCommand(pimCmd)
}
