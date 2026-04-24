package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
)

const (
	cloudAzure = "azure"
	cloudGCP   = "gcp"
	cloudAWS   = "aws"
)

var lsCmd = &cobra.Command{
	Use:     "ls <provider> <kind>",
	Aliases: []string{"list"},
	Short:   "List resources non-interactively (pipeable)",
	Example: `  cloudnav ls azure subs --json
  cloudnav ls azure rgs --subscription 00000000-0000-0000-0000-000000000000
  cloudnav ls azure resources --subscription <id> --resource-group my-rg
  cloudnav ls gcp projects --json
  cloudnav ls gcp resources --project my-project
  cloudnav ls aws account
  cloudnav ls aws regions
  cloudnav ls aws resources --region us-east-1`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := pickProvider(args[0])
		if err != nil {
			return err
		}

		sub, _ := cmd.Flags().GetString("subscription")
		rg, _ := cmd.Flags().GetString("resource-group")
		project, _ := cmd.Flags().GetString("project")
		region, _ := cmd.Flags().GetString("region")
		asJSON, _ := cmd.Flags().GetBool("json")

		ctx := context.Background()
		var nodes []provider.Node

		switch args[1] {
		case "subs", "subscriptions", "projects", "account", "accounts":
			nodes, err = p.Root(ctx)
		case "rgs", "resource-groups":
			if sub == "" {
				return fmt.Errorf("--subscription is required for %q", args[1])
			}
			nodes, err = p.Children(ctx, provider.Node{ID: sub, Kind: provider.KindSubscription})
		case "regions":
			// Uses the current AWS account as implied scope.
			root, e := p.Root(ctx)
			if e != nil {
				return e
			}
			if len(root) == 0 {
				return fmt.Errorf("no account returned from %s", p.Name())
			}
			nodes, err = p.Children(ctx, root[0])
		case "resources":
			switch p.Name() {
			case cloudAzure:
				if sub == "" || rg == "" {
					return fmt.Errorf("azure: --subscription and --resource-group are required")
				}
				nodes, err = p.Children(ctx, provider.Node{
					ID:     rg,
					Name:   rg,
					Kind:   provider.KindResourceGroup,
					Parent: &provider.Node{ID: sub, Kind: provider.KindSubscription},
				})
			case cloudGCP:
				if project == "" {
					return fmt.Errorf("gcp: --project is required")
				}
				nodes, err = p.Children(ctx, provider.Node{ID: project, Name: project, Kind: provider.KindProject})
			case cloudAWS:
				if region == "" {
					return fmt.Errorf("aws: --region is required")
				}
				nodes, err = p.Children(ctx, provider.Node{ID: region, Name: region, Kind: provider.KindRegion})
			}
		default:
			return fmt.Errorf("unknown kind %q", args[1])
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
	case cloudAzure, "az":
		return azure.New(), nil
	case cloudGCP:
		return gcp.New(), nil
	case cloudAWS:
		return aws.New(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

func init() {
	lsCmd.Flags().Bool("json", false, "Emit JSON instead of a plain table")
	lsCmd.Flags().String("subscription", "", "Azure subscription ID")
	lsCmd.Flags().String("resource-group", "", "Azure resource group name")
	lsCmd.Flags().String("project", "", "GCP project ID")
	lsCmd.Flags().String("region", "", "AWS region")
}
