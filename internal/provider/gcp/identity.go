package gcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Identity satisfies provider.Identifier. Resolves Application
// Default Credentials to identify the active principal and infer
// which credential source resolved.
//
// ADC chain (per Google's spec):
//
//  1. GOOGLE_APPLICATION_CREDENTIALS env var → service-account JSON
//     OR external-account JSON (workload identity federation).
//  2. gcloud's well-known location (~/.config/gcloud/application_
//     default_credentials.json), populated by `gcloud auth
//     application-default login`.
//  3. EC2 / Cloud Run / GKE metadata server.
//  4. Workload Identity Federation via per-host config.
func (g *GCP) Identity(ctx context.Context) (provider.Identity, error) {
	method := detectGCPMethod()
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	creds, err := google.FindDefaultCredentials(c, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return provider.Identity{Method: method}, err
	}
	who := readGCPPrincipal(creds.JSON)
	if who == "" {
		// Mint a token + look up the active account via gcloud as a
		// last resort. Some metadata-server / WIF flows don't carry
		// the identity in the credentials JSON.
		out, err := g.gcloud.Run(c, "auth", "list",
			"--filter=status:ACTIVE",
			"--format=value(account)",
		)
		if err == nil {
			who = strings.TrimSpace(string(out))
		}
	}
	return provider.Identity{Who: who, Method: method}, nil
}

// detectGCPMethod sniffs the same signals google.FindDefaultCreds
// uses, in the same priority order, to label the resolved credential
// source. Matches what the SDK actually does so users see a label
// consistent with the underlying behaviour.
func detectGCPMethod() string {
	if v := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); v != "" {
		// Inspect the file to distinguish service-account JSON
		// from external-account / workload identity JSON. Both
		// resolve via GOOGLE_APPLICATION_CREDENTIALS; the
		// `type` field in the JSON tells them apart.
		if t := readGCPCredType(v); t != "" {
			switch t {
			case "service_account":
				return "Service Account JSON (GOOGLE_APPLICATION_CREDENTIALS)"
			case "external_account":
				return "Workload Identity Federation (external_account JSON)"
			case "impersonated_service_account":
				return "Impersonated Service Account JSON"
			default:
				return "ADC JSON file (" + t + ")"
			}
		}
		return "ADC JSON file (GOOGLE_APPLICATION_CREDENTIALS)"
	}
	if onMetadataServer() {
		return "Metadata Server (GCE / Cloud Run / GKE)"
	}
	if home, err := os.UserHomeDir(); err == nil {
		// gcloud's ADC location.
		p := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
		if _, err := os.Stat(p); err == nil {
			if t := readGCPCredType(p); t == "external_account" {
				return "gcloud user creds with WIF"
			}
			return "gcloud cached ADC (`gcloud auth application-default login`)"
		}
	}
	return "Application Default Credentials"
}

func readGCPCredType(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return ""
	}
	return env.Type
}

// readGCPPrincipal extracts the principal email from a credentials
// JSON. Service-account JSON has `client_email`; user creds have
// `account` after gcloud auth. Returns "" when neither is present.
func readGCPPrincipal(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var env struct {
		ClientEmail string `json:"client_email"`
		Account     string `json:"account"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return ""
	}
	if env.ClientEmail != "" {
		return env.ClientEmail
	}
	return env.Account
}

// onMetadataServer reports whether GOOGLE_CLOUD_PROJECT is set
// (typical on GCE / Cloud Run) without doing an actual metadata
// probe — we can't make a 169.254.169.254 request from a doctor
// command without a 100 ms timeout per call. Best-effort
// labelling.
func onMetadataServer() bool {
	return os.Getenv("GCE_METADATA_HOST") != "" ||
		os.Getenv("GOOGLE_CLOUD_PROJECT") != ""
}
