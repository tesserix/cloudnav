package azure

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Identity satisfies provider.Identifier. Probes the active
// DefaultAzureCredential chain and reports both the principal and
// the credential source that resolved.
//
// DefaultAzureCredential tries in this order:
//
//  1. EnvironmentCredential   — AZURE_TENANT_ID + AZURE_CLIENT_ID +
//     AZURE_CLIENT_SECRET     (Service Principal)
//     OR AZURE_FEDERATED_TOKEN_FILE (workload identity)
//  2. WorkloadIdentityCredential — AKS / GitHub Actions OIDC
//  3. ManagedIdentityCredential   — Azure VMs, App Service, Functions
//  4. AzureCLICredential          — `az login` cached token
//  5. AzureDeveloperCLICredential — `azd auth login`
//
// We don't get a typed "which one fired" answer back from the SDK, so
// we sniff the same env signals the chain consults to label the
// method. Everything else falls through to "Azure CLI cached token"
// which matches the most common cloudnav user.
func (a *Azure) Identity(ctx context.Context) (provider.Identity, error) {
	method := detectAzureMethod()
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := a.az.Run(c, "account", "show",
		"--query", "user.name",
		"-o", "tsv",
	)
	if err != nil {
		return provider.Identity{Method: method}, err
	}
	who := strings.TrimSpace(string(out))
	return provider.Identity{Who: who, Method: method}, nil
}

// detectAzureMethod sniffs the env-var signals
// `azidentity.NewDefaultAzureCredential` uses to pick a chain
// member. Order matches the credential-chain order so the first
// match wins, mirroring the SDK's actual behaviour.
func detectAzureMethod() string {
	switch {
	case os.Getenv("AZURE_FEDERATED_TOKEN_FILE") != "":
		return "Workload Identity (federated token file)"
	case os.Getenv("AZURE_CLIENT_ID") != "" && os.Getenv("AZURE_CLIENT_SECRET") != "":
		return "Service Principal (client secret env vars)"
	case os.Getenv("AZURE_CLIENT_ID") != "" && os.Getenv("AZURE_CLIENT_CERTIFICATE_PATH") != "":
		return "Service Principal (client certificate)"
	case os.Getenv("MSI_ENDPOINT") != "" || os.Getenv("IDENTITY_ENDPOINT") != "":
		return "Managed Identity (IMDS)"
	default:
		return "Azure CLI cached token"
	}
}
