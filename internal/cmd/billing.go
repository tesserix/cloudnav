package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider/gcp"
)

var billingCmd = &cobra.Command{
	Use:   "billing",
	Short: "Billing-export setup helpers (GCP)",
	Long:  "Commands that diagnose and help set up cloud-native billing exports. For GCP this walks the BigQuery billing-export readiness chain: billing account, IAM roles, dataset, export table.",
}

var billingStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report billing-export setup status (GCP only)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		g := gcp.New()
		st, err := g.BillingStatus(cmd.Context())
		if err != nil && st == nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		fmt.Printf("project          %s\n", st.Project)
		fmt.Printf("billing account  %s  (enabled=%v)\n", nonempty(st.BillingAccount, "unknown"), st.BillingEnabled)
		fmt.Printf("your roles       %v\n", st.Roles)
		fmt.Printf("  can admin      %v (need roles/billing.admin or roles/billing.accountsCostManager to enable export)\n", st.CanAdminBilling)
		fmt.Printf("dataset %q      exists=%v\n", st.Dataset, st.DatasetExists)
		if st.ExportTable != "" {
			fmt.Printf("export table     ✓ %s\n", st.ExportTable)
			fmt.Println("\n✓ BigQuery billing export is live. cloudnav will pick it up automatically.")
		} else {
			fmt.Println("export table     ✗ not found")
			fmt.Println()
			fmt.Println("next steps:")
			if !st.DatasetExists {
				fmt.Println("  1. cloudnav billing init  (creates the 'billing_export' dataset in this project)")
				fmt.Printf("  2. open %s and configure the 'Detailed usage cost' export to dataset billing_export\n", st.SetupURL)
				fmt.Println("  3. wait a few hours for the first export to land, then re-run `cloudnav billing status`")
			} else {
				fmt.Printf("  1. open %s and configure the 'Detailed usage cost' export to dataset %s\n", st.SetupURL, st.Dataset)
				fmt.Println("  2. wait a few hours for the first export to land, then re-run `cloudnav billing status`")
			}
		}
		return nil
	},
}

var billingInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create the 'billing_export' BigQuery dataset (GCP)",
	Long:  "Creates the conventional billing_export BigQuery dataset in the gcloud default project. Requires roles/billing.admin or roles/billing.accountsCostManager. The export itself is Google-console-only (no public API); this command just handles the dataset so the console dropdown is pre-populated.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		g := gcp.New()
		path, err := g.InitBillingDataset(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Printf("✓ dataset ready: %s\n\n", path)
		fmt.Println("next: finish the export in the billing console, then `cloudnav billing status`.")
		return nil
	},
}

func nonempty(a, fallback string) string {
	if a == "" {
		return fallback
	}
	return a
}

func init() {
	billingStatusCmd.Flags().Bool("json", false, "Emit JSON")
	billingCmd.AddCommand(billingStatusCmd, billingInitCmd)
	rootCmd.AddCommand(billingCmd)
}
