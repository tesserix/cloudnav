package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("cloudnav", version.String())
	},
}
