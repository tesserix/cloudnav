package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
)

var vmCmd = &cobra.Command{
	Use:   "vm",
	Short: "List, inspect, and control virtual machines across clouds",
	Long: `Read-only by default. The start / stop subcommands mutate state and
require --yes to run.

  cloudnav vm list  --cloud azure --subscription <id> [--resource-group <rg>]
  cloudnav vm list  --cloud gcp   --project <id>
  cloudnav vm list  --cloud aws   --region us-east-1

  cloudnav vm show  <id> --cloud azure
  cloudnav vm show  <id> --cloud gcp --scope <project>/<zone>
  cloudnav vm show  <id> --cloud aws --region us-east-1

  cloudnav vm start <id...> --cloud aws --region us-east-1 --yes
  cloudnav vm stop  <id...> --cloud aws --region us-east-1 --yes`,
}

func vmPickOps(cloud string) (provider.VMOps, error) {
	switch cloud {
	case cloudAzure, "az":
		return azure.New(), nil
	case cloudGCP:
		return gcp.New(), nil
	case cloudAWS:
		return aws.New(), nil
	}
	return nil, fmt.Errorf("unknown cloud %q", cloud)
}

var vmListCmd = &cobra.Command{
	Use:   "list",
	Short: "List VMs / EC2 / GCE instances in a scope",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		sub, _ := cmd.Flags().GetString("subscription")
		rg, _ := cmd.Flags().GetString("resource-group")
		project, _ := cmd.Flags().GetString("project")
		region, _ := cmd.Flags().GetString("region")
		asJSON, _ := cmd.Flags().GetBool("json")
		state, _ := cmd.Flags().GetString("state")

		ops, err := vmPickOps(cloud)
		if err != nil {
			return err
		}
		scope, err := vmScope(cloud, sub, rg, project, region)
		if err != nil {
			return err
		}
		vms, err := ops.ListVMs(cmd.Context(), scope)
		if err != nil {
			return err
		}
		if state != "" {
			out := vms[:0]
			for _, v := range vms {
				if strings.EqualFold(v.State, state) {
					out = append(out, v)
				}
			}
			vms = out
		}
		sort.Slice(vms, func(i, j int) bool { return vms[i].Name < vms[j].Name })

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(vms)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "NAME\tTYPE\tSTATE\tLOCATION\tID")
		println(tw, "----\t----\t-----\t--------\t--")
		for _, v := range vms {
			printf(tw, "%s\t%s\t%s\t%s\t%s\n",
				trunc(v.Name, 40),
				trunc(v.Type, 22),
				v.State,
				v.Location,
				trunc(v.ID, 60),
			)
		}
		println(tw, "----\t----\t-----\t--------\t--")
		printf(tw, "%d vm(s)\t\t\t\t\n", len(vms))
		return tw.Flush()
	},
}

var vmShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Print the full metadata JSON for a VM",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cloud, _ := cmd.Flags().GetString("cloud")
		scope, _ := cmd.Flags().GetString("scope")
		region, _ := cmd.Flags().GetString("region")
		ops, err := vmPickOps(cloud)
		if err != nil {
			return err
		}
		arg := scope
		if cloud == cloudAWS {
			arg = region
		}
		out, err := ops.ShowVM(cmd.Context(), args[0], arg)
		if err != nil {
			return err
		}
		_, _ = os.Stdout.Write(out)
		return nil
	},
}

var vmStartCmd = &cobra.Command{
	Use:   "start <id>...",
	Short: "Start one or more VMs (mutating — requires --yes)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return vmBatch(cmd, args, "start")
	},
}

var vmStopCmd = &cobra.Command{
	Use:   "stop <id>...",
	Short: "Stop / deallocate one or more VMs (mutating — requires --yes)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return vmBatch(cmd, args, "stop")
	},
}

func vmBatch(cmd *cobra.Command, ids []string, action string) error {
	cloud, _ := cmd.Flags().GetString("cloud")
	scope, _ := cmd.Flags().GetString("scope")
	region, _ := cmd.Flags().GetString("region")
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		return fmt.Errorf("%s on %d instance(s) requires --yes to proceed", action, len(ids))
	}
	ops, err := vmPickOps(cloud)
	if err != nil {
		return err
	}
	arg := scope
	if cloud == cloudAWS {
		arg = region
	}

	failures := 0
	for _, id := range ids {
		var err error
		switch action {
		case "start":
			err = ops.StartVM(cmd.Context(), id, arg)
		case "stop":
			err = ops.StopVM(cmd.Context(), id, arg)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s %s: %v\n", action, id, err)
			failures++
			continue
		}
		fmt.Printf("✓ %s requested: %s\n", action, id)
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d %s operations failed", failures, len(ids), action)
	}
	return nil
}

func vmScope(cloud, sub, rg, project, region string) (provider.Node, error) {
	switch cloud {
	case cloudAzure, "az":
		if rg != "" {
			return provider.Node{Name: rg, Kind: provider.KindResourceGroup, Meta: map[string]string{"subscriptionId": sub}}, nil
		}
		if sub == "" {
			return provider.Node{}, fmt.Errorf("azure: --subscription or --resource-group is required")
		}
		return provider.Node{ID: sub, Kind: provider.KindSubscription}, nil
	case cloudGCP:
		if project == "" {
			return provider.Node{}, fmt.Errorf("gcp: --project is required")
		}
		return provider.Node{ID: project, Kind: provider.KindProject}, nil
	case cloudAWS:
		if region == "" {
			return provider.Node{}, fmt.Errorf("aws: --region is required")
		}
		return provider.Node{ID: region, Kind: provider.KindRegion}, nil
	}
	return provider.Node{}, fmt.Errorf("unknown cloud %q", cloud)
}

func init() {
	vmCmd.PersistentFlags().String("cloud", cloudAzure, "cloud: azure | aws | gcp")
	vmCmd.PersistentFlags().String("subscription", "", "Azure subscription ID")
	vmCmd.PersistentFlags().String("resource-group", "", "Azure resource group (optional)")
	vmCmd.PersistentFlags().String("project", "", "GCP project ID")
	vmCmd.PersistentFlags().String("region", "", "AWS region")
	vmCmd.PersistentFlags().String("scope", "", "GCP vm scope for show/start/stop: project/zone")
	vmCmd.PersistentFlags().Bool("yes", false, "Confirm a mutating operation")

	vmListCmd.Flags().Bool("json", false, "Emit JSON")
	vmListCmd.Flags().String("state", "", "Filter by state (running, stopped, ...)")

	vmCmd.AddCommand(vmListCmd, vmShowCmd, vmStartCmd, vmStopCmd)
	rootCmd.AddCommand(vmCmd)
}
