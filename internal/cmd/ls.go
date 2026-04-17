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

var lsCmd = &cobra.Command{
	Use:   "ls <provider> <kind>",
	Short: "List resources non-interactively (pipeable)",
	Example: `  cloudnav ls azure subs --json
  cloudnav ls azure rgs --subscription 00000000-0000-0000-0000-000000000000
  cloudnav ls azure resources --subscription <id> --resource-group my-rg`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := pickProvider(args[0])
		if err != nil {
			return err
		}

		sub, _ := cmd.Flags().GetString("subscription")
		rg, _ := cmd.Flags().GetString("resource-group")
		asJSON, _ := cmd.Flags().GetBool("json")

		ctx := context.Background()
		var nodes []provider.Node

		switch args[1] {
		case "subs", "subscriptions":
			nodes, err = p.Root(ctx)
		case "rgs", "resource-groups":
			if sub == "" {
				return fmt.Errorf("--subscription is required for %q", args[1])
			}
			nodes, err = p.Children(ctx, provider.Node{ID: sub, Kind: provider.KindSubscription})
		case "resources":
			if sub == "" || rg == "" {
				return fmt.Errorf("--subscription and --resource-group are required for %q", args[1])
			}
			parent := provider.Node{
				ID:     rg,
				Name:   rg,
				Kind:   provider.KindResourceGroup,
				Parent: &provider.Node{ID: sub, Kind: provider.KindSubscription},
			}
			nodes, err = p.Children(ctx, parent)
		default:
			return fmt.Errorf("unknown kind %q (want: subs | rgs | resources)", args[1])
		}
		if err != nil {
			return err
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(nodes)
		}
		for _, n := range nodes {
			fmt.Printf("%-48s  %s\n", n.Name, n.ID)
		}
		return nil
	},
}

func pickProvider(name string) (provider.Provider, error) {
	switch name {
	case "azure", "az":
		return azure.New(), nil
	case "gcp":
		return nil, fmt.Errorf("gcp provider coming soon")
	case "aws":
		return nil, fmt.Errorf("aws provider coming soon")
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

func init() {
	lsCmd.Flags().Bool("json", false, "Emit JSON instead of a plain table")
	lsCmd.Flags().String("subscription", "", "Azure subscription ID")
	lsCmd.Flags().String("resource-group", "", "Azure resource group name")
}
