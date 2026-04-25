package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
	"github.com/tesserix/cloudnav/internal/provider/aws"
	"github.com/tesserix/cloudnav/internal/provider/azure"
	"github.com/tesserix/cloudnav/internal/provider/gcp"
)

// doctorProbe is one (cloud, optional CLI fallback) entry in the
// doctor checklist. Identifier-based SDK probes run first; the CLI
// fallback covers hosts where the SDK chain can't auth (no
// ~/.aws / ~/.config/gcloud / no service-principal env vars / no
// CLI cached token, etc.).
type doctorProbe struct {
	name        string
	cliBin      string
	cliCheck    []string
	installHint string
	provider    provider.Provider
}

var doctorProbes = []doctorProbe{
	{
		name:        cloudAzure,
		cliBin:      "az",
		cliCheck:    []string{"account", "show", "--query", "user.name", "-o", "tsv"},
		installHint: "https://learn.microsoft.com/cli/azure/install-azure-cli",
		provider:    azure.New(),
	},
	{
		name:        cloudGCP,
		cliBin:      "gcloud",
		cliCheck:    []string{"auth", "list", "--filter=status:ACTIVE", "--format=value(account)"},
		installHint: "https://cloud.google.com/sdk/docs/install",
		provider:    gcp.New(),
	},
	{
		name:        cloudAWS,
		cliBin:      "aws",
		cliCheck:    []string{"sts", "get-caller-identity", "--query", "Arn", "--output", "text"},
		installHint: "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html",
		provider:    aws.New(),
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that each cloud is reachable and report which auth method is active",
	Long: `For every cloud (Azure / GCP / AWS) cloudnav probes the active
authentication chain and reports:

  - the principal that authenticated (user email, Service
    Principal id, IAM ARN);
  - the credential source that resolved (CLI cached token,
    Service Principal env vars, federated workload identity,
    Managed Identity / metadata server, IRSA, etc.).

The probe runs through the cloud's SDK auth chain first (so SP /
federated / OIDC users see a green check even without a CLI
installed) and falls back to the CLI-based check when the SDK
can't resolve credentials.

See docs/auth.md for every supported method per cloud.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()

		missing := []string{}
		notAuthed := []string{}
		for _, p := range doctorProbes {
			ok, missingCli := runProbe(ctx, p)
			switch {
			case !ok && missingCli:
				fmt.Printf("✗ %-6s %s not installed and SDK chain has no creds  · %s\n",
					p.name, p.cliBin, p.installHint)
				missing = append(missing, p.name)
			case !ok:
				notAuthed = append(notAuthed, p.name)
			}
		}
		if len(missing) == 0 && len(notAuthed) == 0 {
			return nil
		}
		fmt.Println()
		if len(missing) > 0 {
			fmt.Printf("next step — `cloudnav install <cloud>` for: %v\n", missing)
		}
		if len(notAuthed) > 0 {
			fmt.Printf("next step — run `cloudnav login <cloud>` for: %v\n", notAuthed)
		}
		return nil
	},
}

// runProbe is the per-cloud check. Returns (ok, missingCli):
//
//	ok          - at least one path (Identifier or CLI) authenticated.
//	missingCli  - the CLI fallback couldn't run because the binary
//	              isn't on PATH (only relevant when the Identifier
//	              probe also failed).
//
// Both an Identifier success and a CLI success print a "✓" row;
// the caller only handles the negative cases below.
func runProbe(ctx context.Context, p doctorProbe) (ok, missingCli bool) {
	if id, hit := tryIdentity(ctx, p.provider); hit {
		fmt.Printf("✓ %-6s %s  · %s\n", p.name, id.Who, id.Method)
		return true, false
	}
	if _, err := exec.LookPath(p.cliBin); err != nil {
		return false, true
	}
	out, err := exec.CommandContext(ctx, p.cliBin, p.cliCheck...).CombinedOutput()
	if err != nil {
		fmt.Printf("✗ %-6s installed but not authenticated → `cloudnav login %s`\n", p.name, p.name)
		return false, false
	}
	who := firstLine(string(bytesTrim(out)))
	if who == "" {
		who = "logged in"
	}
	fmt.Printf("✓ %-6s %s  · %s CLI cached token\n", p.name, who, p.cliBin)
	return true, false
}

// tryIdentity calls the optional Identifier interface. Returns
// (id, true) on a clean response with a non-empty principal.
// Empty principal or any error counts as a soft fail so the
// caller falls through to the CLI probe rather than printing a
// half-populated row.
func tryIdentity(ctx context.Context, p provider.Provider) (provider.Identity, bool) {
	ider, ok := p.(provider.Identifier)
	if !ok {
		return provider.Identity{}, false
	}
	id, err := ider.Identity(ctx)
	if err != nil || id.Who == "" {
		return provider.Identity{}, false
	}
	return id, true
}

func bytesTrim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
