package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider/azure"
)

var advisorCmd = &cobra.Command{
	Use:   "advisor",
	Short: "Azure Advisor recommendations (Cost / Security / HA / Performance / Operational Excellence)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		sub, _ := cmd.Flags().GetString("subscription")
		asJSON, _ := cmd.Flags().GetBool("json")
		category, _ := cmd.Flags().GetString("category")
		minImpact, _ := cmd.Flags().GetString("impact")
		if sub == "" {
			return fmt.Errorf("--subscription is required")
		}

		az := azure.New()
		recs, err := az.Recommendations(cmd.Context(), sub)
		if err != nil {
			return err
		}

		filtered := recs[:0]
		for _, r := range recs {
			if category != "" && !strings.EqualFold(r.Category, category) {
				continue
			}
			if minImpact != "" && !meetsImpact(r.Impact, minImpact) {
				continue
			}
			filtered = append(filtered, r)
		}
		sort.Slice(filtered, func(i, j int) bool {
			if impactRank(filtered[i].Impact) != impactRank(filtered[j].Impact) {
				return impactRank(filtered[i].Impact) > impactRank(filtered[j].Impact)
			}
			return filtered[i].Category < filtered[j].Category
		})

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(filtered)
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		println(tw, "IMPACT\tCATEGORY\tTARGET\tPROBLEM")
		println(tw, "------\t--------\t------\t-------")
		for _, r := range filtered {
			printf(tw, "%s\t%s\t%s\t%s\n",
				r.Impact,
				r.Category,
				trunc(r.ImpactedName, 30),
				trunc(r.Problem, 80),
			)
		}
		println(tw, "------\t--------\t------\t-------")
		printf(tw, "%d\trecommendation(s)\t\t\n", len(filtered))
		return tw.Flush()
	},
}

func impactRank(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func meetsImpact(actual, min string) bool {
	return impactRank(actual) >= impactRank(min)
}

func init() {
	advisorCmd.Flags().String("subscription", "", "Azure subscription ID (required)")
	advisorCmd.Flags().String("category", "", "Filter by category: Cost | Security | HighAvailability | Performance | OperationalExcellence")
	advisorCmd.Flags().String("impact", "", "Filter by minimum impact: Low | Medium | High")
	advisorCmd.Flags().Bool("json", false, "Emit JSON")
	rootCmd.AddCommand(advisorCmd)
}
