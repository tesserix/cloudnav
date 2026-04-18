package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// BillingStatus is the machine-readable diagnostic the TUI and the
// `cloudnav billing status` CLI render. It walks the setup chain — billing
// account, IAM roles, BigQuery dataset, export table — and reports exactly
// which step is pending. Intentionally a value type so it round-trips
// cleanly through JSON.
type BillingStatus struct {
	Project         string   `json:"project"`
	BillingAccount  string   `json:"billingAccount"`
	BillingEnabled  bool     `json:"billingEnabled"`
	Roles           []string `json:"roles"`
	CanAdminBilling bool     `json:"canAdminBilling"`
	DatasetExists   bool     `json:"datasetExists"`
	Dataset         string   `json:"dataset"`
	ExportTable     string   `json:"exportTable,omitempty"` // populated when the export is live
	SetupURL        string   `json:"setupUrl"`
}

// BillingStatus walks the billing-export readiness chain for the gcloud
// default project and returns a diagnostic the caller can present as a
// checklist.
func (g *GCP) BillingStatus(ctx context.Context) (*BillingStatus, error) {
	out, err := g.gcloud.Run(ctx, "config", "get-value", "project")
	if err != nil {
		return nil, fmt.Errorf("resolve gcloud default project: %w", err)
	}
	project := strings.TrimSpace(string(out))
	if project == "" {
		return nil, fmt.Errorf("no gcloud default project set — run: gcloud config set project <id>")
	}
	status := &BillingStatus{Project: project, Dataset: "billing_export"}

	// 1. Billing account linked to this project.
	biOut, err := g.gcloud.Run(ctx, "billing", "projects", "describe", project, "--format=json")
	if err != nil {
		return status, fmt.Errorf("gcloud billing projects describe %s: %w", project, err)
	}
	var bi struct {
		BillingAccountName string `json:"billingAccountName"`
		BillingEnabled     bool   `json:"billingEnabled"`
	}
	if err := json.Unmarshal(biOut, &bi); err != nil {
		return status, fmt.Errorf("parse billing info: %w", err)
	}
	status.BillingAccount = strings.TrimPrefix(bi.BillingAccountName, "billingAccounts/")
	status.BillingEnabled = bi.BillingEnabled
	status.SetupURL = fmt.Sprintf("https://console.cloud.google.com/billing/%s/export", status.BillingAccount)
	if status.BillingAccount == "" {
		return status, nil
	}

	// 2. IAM on the billing account — do we have the roles to enable export?
	if roles, err := g.callerBillingRoles(ctx, status.BillingAccount); err == nil {
		status.Roles = roles
		for _, r := range roles {
			if r == "roles/billing.admin" || r == "roles/billing.accountsCostManager" {
				status.CanAdminBilling = true
				break
			}
		}
	}

	// 3. Dataset + table existence.
	if _, err := g.gcloud.Run(ctx, "alpha", "bq", "datasets", "describe",
		"--project="+project,
		status.Dataset,
		"--format=value(datasetReference.datasetId)",
	); err == nil {
		status.DatasetExists = true
	}
	if status.DatasetExists {
		tableSuffix := strings.ReplaceAll(status.BillingAccount, "-", "_")
		if _, err := g.gcloud.Run(ctx, "alpha", "bq", "tables", "describe",
			"--dataset="+status.Dataset,
			"--project="+project,
			"gcp_billing_export_v1_"+tableSuffix,
			"--format=value(tableReference.tableId)",
		); err == nil {
			status.ExportTable = fmt.Sprintf("%s.%s.gcp_billing_export_v1_%s", project, status.Dataset, tableSuffix)
		}
	}
	return status, nil
}

// callerBillingRoles returns the roles the signed-in user holds on the
// billing account, via the billing IAM policy. Non-admin callers get
// Forbidden here; we swallow that so the diagnostic still renders without
// the roles section.
func (g *GCP) callerBillingRoles(ctx context.Context, billingAccount string) ([]string, error) {
	who, err := g.gcloud.Run(ctx, "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
	if err != nil {
		return nil, err
	}
	email := strings.TrimSpace(string(who))
	if email == "" {
		return nil, fmt.Errorf("no active gcloud account")
	}
	out, err := g.gcloud.Run(ctx,
		"billing", "accounts", "get-iam-policy", billingAccount,
		"--format=json",
	)
	if err != nil {
		return nil, err
	}
	var pol struct {
		Bindings []struct {
			Role    string   `json:"role"`
			Members []string `json:"members"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(out, &pol); err != nil {
		return nil, err
	}
	roles := []string{}
	target := "user:" + email
	for _, b := range pol.Bindings {
		for _, m := range b.Members {
			if m == target {
				roles = append(roles, b.Role)
			}
		}
	}
	return roles, nil
}

// InitBillingDataset creates the conventional "billing_export" BigQuery
// dataset in the gcloud default project when it doesn't exist yet. Returns
// the fully-qualified dataset path on success. The BQ export itself still
// has to be enabled from the billing console (Google doesn't expose that
// toggle via API), but this saves the user one manual step and makes the
// console-side "dataset" dropdown pre-populated.
func (g *GCP) InitBillingDataset(ctx context.Context) (string, error) {
	st, err := g.BillingStatus(ctx)
	if err != nil {
		return "", err
	}
	if !st.CanAdminBilling {
		return "", fmt.Errorf("need roles/billing.admin or billing.accountsCostManager on %s to enable export — current roles: %v", st.BillingAccount, st.Roles)
	}
	if st.DatasetExists {
		return fmt.Sprintf("%s.%s", st.Project, st.Dataset), nil
	}
	if _, err := g.gcloud.Run(ctx,
		"alpha", "bq", "datasets", "create",
		"--project="+st.Project,
		"--location=US",
		"--description=cloudnav-created dataset for Cloud Billing export",
		st.Dataset,
	); err != nil {
		return "", fmt.Errorf("create dataset %s.%s: %w", st.Project, st.Dataset, err)
	}
	return fmt.Sprintf("%s.%s", st.Project, st.Dataset), nil
}
