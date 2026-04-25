package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
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

func println(w io.Writer, s string)          { _, _ = fmt.Fprintln(w, s) }
func printf(w io.Writer, f string, a ...any) { _, _ = fmt.Fprintf(w, f, a...) }

var costCmd = &cobra.Command{
	Use:     "cost",
	Aliases: []string{"costs"},
	Short:   "Read-only cost reports across subs / RGs / regions / services",
	Long:    "cloudnav never creates or modifies cloud resources. `cost` runs read-only CostManagement / Cost Explorer / BigQuery queries and prints them as a table.",
}

var costSubsCmd = &cobra.Command{
	Use:   "subs",
	Short: "Cost per subscription (Azure)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		limit, _ := cmd.Flags().GetInt("limit")
		match, _ := cmd.Flags().GetString("match")

		az := azure.New()
		ctx := cmd.Context()
		subs, err := az.Root(ctx)
		if err != nil {
			return err
		}
		filtered := make([]provider.Node, 0, len(subs))
		for _, s := range subs {
			if match == "" || strings.Contains(strings.ToLower(s.Name), strings.ToLower(match)) {
				filtered = append(filtered, s)
			}
		}
		if limit > 0 && len(filtered) > limit {
			filtered = filtered[:limit]
		}
		ids := make([]string, 0, len(filtered))
		nameByID := map[string]string{}
		for _, s := range filtered {
			ids = append(ids, s.ID)
			nameByID[s.ID] = s.Name
		}
		fmt.Fprintf(os.Stderr, "querying cost for %d subscription(s)...\n", len(ids))
		costs, _ := az.SubscriptionCosts(ctx, ids)

		sort.Slice(costs, func(i, j int) bool { return costs[i].Current > costs[j].Current })

		if asJSON {
			type row struct {
				SubscriptionID   string  `json:"subscription_id"`
				SubscriptionName string  `json:"subscription_name"`
				CurrentMTD       float64 `json:"current_mtd"`
				LastMonthToDate  float64 `json:"last_month_to_date"`
				DeltaPct         float64 `json:"delta_pct"`
				Currency         string  `json:"currency"`
			}
			out := make([]row, 0, len(costs))
			for _, c := range costs {
				out = append(out, row{
					SubscriptionID:   c.SubscriptionID,
					SubscriptionName: nameByID[c.SubscriptionID],
					CurrentMTD:       c.Current,
					LastMonthToDate:  c.LastMonth,
					DeltaPct:         pctDelta(c.Current, c.LastMonth),
					Currency:         c.Currency,
				})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "SUBSCRIPTION\tID\tMTD\tLAST MTD\tΔ")
		println(tw, "------------\t--\t---\t--------\t--")
		var totCur, totLast float64
		currency := ""
		errored := 0
		for _, c := range costs {
			if c.Error != "" {
				errored++
				printf(tw, "%s\t%s\t%s\t%s\t%s\n",
					trunc(nameByID[c.SubscriptionID], 44),
					shortUUID(c.SubscriptionID),
					"—", "—", c.Error,
				)
				continue
			}
			if currency == "" {
				currency = c.Currency
			}
			printf(tw, "%s\t%s\t%s\t%s\t%s\n",
				trunc(nameByID[c.SubscriptionID], 44),
				shortUUID(c.SubscriptionID),
				formatAmount(c.Current, c.Currency),
				formatAmount(c.LastMonth, c.Currency),
				deltaArrow(c.Current, c.LastMonth),
			)
			totCur += c.Current
			totLast += c.LastMonth
		}
		println(tw, "------------\t--\t---\t--------\t--")
		printf(tw, "TOTAL (%d/%d)\t\t%s\t%s\t%s\n",
			len(costs)-errored, len(costs),
			formatAmount(totCur, currency),
			formatAmount(totLast, currency),
			deltaArrow(totCur, totLast),
		)
		if errored > 0 {
			fmt.Fprintf(os.Stderr, "\n%d sub(s) had no cost data (missing Cost Management Reader on those scopes)\n", errored)
		}
		return tw.Flush()
	},
}

var costRgsCmd = &cobra.Command{
	Use:   "rgs",
	Short: "Cost per resource group in a subscription (Azure)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		sub, _ := cmd.Flags().GetString("subscription")
		asJSON, _ := cmd.Flags().GetBool("json")
		if sub == "" {
			return fmt.Errorf("--subscription is required")
		}
		az := azure.New()
		costs, err := az.Costs(cmd.Context(), provider.Node{ID: sub, Kind: provider.KindSubscription})
		if err != nil {
			return err
		}
		type row struct {
			ResourceGroup string `json:"resource_group"`
			Cost          string `json:"cost"`
		}
		rows := make([]row, 0, len(costs))
		for rg, c := range costs {
			rows = append(rows, row{ResourceGroup: rg, Cost: c})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ResourceGroup < rows[j].ResourceGroup })
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "RESOURCE GROUP\tMTD  (Δ vs last month)")
		println(tw, "--------------\t---------------------")
		for _, r := range rows {
			printf(tw, "%s\t%s\n", r.ResourceGroup, r.Cost)
		}
		return tw.Flush()
	},
}

var costRegionsCmd = &cobra.Command{
	Use:   "regions",
	Short: "Cost per region for the calling AWS account",
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		a := aws.New()
		ctx := cmd.Context()
		accounts, err := a.Root(ctx)
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			return fmt.Errorf("aws: sts get-caller-identity returned no account")
		}
		costs, err := a.Costs(ctx, accounts[0])
		if err != nil {
			return err
		}
		type row struct {
			Region string `json:"region"`
			Cost   string `json:"cost"`
		}
		rows := make([]row, 0, len(costs))
		for r, c := range costs {
			rows = append(rows, row{Region: r, Cost: c})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Region < rows[j].Region })
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "REGION\tMTD  (Δ vs last month)")
		println(tw, "------\t---------------------")
		for _, r := range rows {
			printf(tw, "%s\t%s\n", r.Region, r.Cost)
		}
		return tw.Flush()
	},
}

var costProjectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Cost per project (GCP) — reads the BigQuery billing-export table",
	Long: `Lists month-to-date cost per GCP project, sourced from the BigQuery
billing-export table cloudnav resolves via gcp.billing_table in
config.json or the CLOUDNAV_GCP_BILLING_TABLE env var.

If neither is set cloudnav auto-detects the canonical
'<project>.billing_export.gcp_billing_export_v1_<account>' table; if
that's not present either, the command prints the setup deeplink so
you can enable export from the console and re-run.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		match, _ := cmd.Flags().GetString("match")
		limit, _ := cmd.Flags().GetInt("limit")

		g := gcp.New()
		ctx := cmd.Context()
		// Coster on GCP keys by Kind=Cloud — root scope returns
		// project_id → cost across the active billing account.
		costs, err := g.Costs(ctx, provider.Node{Kind: provider.KindCloud})
		if err != nil {
			return err
		}
		// Project IDs come from the costs map; we don't ship a name
		// here because the BQ payload doesn't carry display names —
		// users who want names can pipe the JSON through `gcloud
		// projects describe`.
		type row struct {
			ProjectID string `json:"project_id"`
			Cost      string `json:"cost"`
		}
		rows := make([]row, 0, len(costs))
		for pid, c := range costs {
			if match != "" && !strings.Contains(strings.ToLower(pid), strings.ToLower(match)) {
				continue
			}
			rows = append(rows, row{ProjectID: pid, Cost: c})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ProjectID < rows[j].ProjectID })
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "PROJECT\tMTD")
		println(tw, "-------\t---")
		for _, r := range rows {
			printf(tw, "%s\t%s\n", r.ProjectID, r.Cost)
		}
		println(tw, "-------\t---")
		printf(tw, "TOTAL (%d project(s))\t\n", len(rows))
		return tw.Flush()
	},
}

var costServicesCmd = &cobra.Command{
	Use:   "services",
	Short: "Cost per AWS service across the calling account",
	RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		a := aws.New()
		services, err := a.ServiceCosts(cmd.Context())
		if err != nil {
			return err
		}
		sort.Slice(services, func(i, j int) bool { return services[i].Current > services[j].Current })

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(services)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "SERVICE\tMTD\tLAST MTD\tΔ")
		println(tw, "-------\t---\t--------\t--")
		var totCur, totLast float64
		currency := ""
		for _, s := range services {
			if currency == "" {
				currency = s.Currency
			}
			printf(tw, "%s\t%s\t%s\t%s\n",
				s.Service,
				formatAmount(s.Current, s.Currency),
				formatAmount(s.LastMonth, s.Currency),
				deltaArrow(s.Current, s.LastMonth),
			)
			totCur += s.Current
			totLast += s.LastMonth
		}
		println(tw, "-------\t---\t--------\t--")
		printf(tw, "TOTAL\t%s\t%s\t%s\n",
			formatAmount(totCur, currency),
			formatAmount(totLast, currency),
			deltaArrow(totCur, totLast),
		)
		return tw.Flush()
	},
}

func formatAmount(amount float64, currency string) string {
	symbol := currencySymbolCLI(currency)
	return fmt.Sprintf("%s%.2f", symbol, amount)
}

func deltaArrow(current, last float64) string {
	if last == 0 {
		if current == 0 {
			return "—"
		}
		return "new"
	}
	pct := (current - last) / last * 100
	switch {
	case pct > 2:
		return fmt.Sprintf("↑%d%%", int(math.Round(pct)))
	case pct < -2:
		return fmt.Sprintf("↓%d%%", int(math.Round(-pct)))
	default:
		return "→"
	}
}

func pctDelta(current, last float64) float64 {
	if last == 0 {
		if current == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (current - last) / last * 100
}

func currencySymbolCLI(code string) string {
	switch strings.ToUpper(code) {
	case "USD", "":
		return "$"
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "INR":
		return "₹"
	case "JPY":
		return "¥"
	default:
		return code + " "
	}
}

func shortUUID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func init() {
	costCmd.AddCommand(costSubsCmd, costRgsCmd, costRegionsCmd, costServicesCmd, costProjectsCmd)
	costSubsCmd.Flags().Bool("json", false, "Emit JSON")
	costSubsCmd.Flags().String("match", "", "Substring filter on subscription name")
	costSubsCmd.Flags().Int("limit", 0, "Maximum subscriptions to include after filtering (0 = all)")
	costRgsCmd.Flags().Bool("json", false, "Emit JSON")
	costRgsCmd.Flags().String("subscription", "", "Azure subscription ID (required)")
	costRegionsCmd.Flags().Bool("json", false, "Emit JSON")
	costServicesCmd.Flags().Bool("json", false, "Emit JSON")
	costProjectsCmd.Flags().Bool("json", false, "Emit JSON")
	costProjectsCmd.Flags().String("match", "", "Substring filter on project ID")
	costProjectsCmd.Flags().Int("limit", 0, "Maximum projects to include (0 = all)")
	rootCmd.AddCommand(costCmd)
}
