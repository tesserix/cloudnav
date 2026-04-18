package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
)

var loginCmd = &cobra.Command{
	Use:       "login [cloud]",
	Short:     "Run the selected cloud CLI's interactive login (az / gcloud / aws)",
	Long:      "Launches the cloud CLI's native login flow so first-time users can get credentials without memorizing which command each cloud uses. Pass 'azure', 'aws', or 'gcp'.",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"azure", "aws", "gcp"},
	RunE: func(cmd *cobra.Command, args []string) error {
		var p provider.Provider
		switch args[0] {
		case "azure":
			p = azure.New()
		case "aws":
			p = aws.New()
		case "gcp":
			p = gcp.New()
		default:
			return fmt.Errorf("unknown cloud %q — expected azure, aws, or gcp", args[0])
		}
		l, ok := p.(provider.Loginer)
		if !ok {
			return fmt.Errorf("%s: login flow not implemented", args[0])
		}
		bin, arg := l.LoginCommand()
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s CLI not found in PATH\n\n%s", p.Name(), l.InstallHint())
		}
		c := exec.Command(bin, arg...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		fmt.Fprintf(os.Stderr, "→ running %s %v\n", bin, arg)
		return c.Run()
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
