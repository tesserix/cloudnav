// Package aws implements provider.Provider by wrapping the `aws` CLI.
package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type AWS struct {
	aws *cli.Runner
}

func New() *AWS {
	r := cli.New("aws")
	r.Timeout = 2 * time.Minute
	return &AWS{aws: r}
}

func (a *AWS) Name() string { return "aws" }

const consoleHome = "https://console.aws.amazon.com/"

func (a *AWS) LoggedIn(ctx context.Context) error {
	_, err := a.aws.Run(ctx, "sts", "get-caller-identity", "--output", "json")
	return err
}

// LoginCommand returns the argv that runs AWS SSO login. Falls back to the
// classic credentials prompt (`aws configure`) when the user isn't using SSO.
func (a *AWS) LoginCommand() (string, []string) {
	return "aws", []string{"sso", "login"}
}

// InstallHint points first-time users at the AWS CLI installer.
func (a *AWS) InstallHint() string {
	return "install AWS CLI: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
}

// InstallPlan picks a per-OS install method, preferring Homebrew where
// available (no sudo, clean uninstall).
func (a *AWS) InstallPlan(goos string) ([]provider.InstallStep, bool) {
	switch goos {
	case "darwin":
		return []provider.InstallStep{{
			Description: "brew install awscli",
			Bin:         "brew", Args: []string{"install", "awscli"},
		}}, true
	case "linux":
		if _, err := exec.LookPath("brew"); err == nil {
			return []provider.InstallStep{{
				Description: "brew install awscli",
				Bin:         "brew", Args: []string{"install", "awscli"},
			}}, true
		}
		return []provider.InstallStep{{
			Description: "download and install AWS CLI v2 (will prompt for sudo)",
			Bin:         "sh", Args: []string{
				"-c",
				`set -e; tmp=$(mktemp -d); cd "$tmp"; curl -sSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o awscliv2.zip; unzip -q awscliv2.zip; sudo ./aws/install; cd /; rm -rf "$tmp"`,
			},
			NeedsSudo: true,
		}}, true
	case "windows":
		return []provider.InstallStep{{
			Description: "winget install Amazon.AWSCLI",
			Bin:         "winget", Args: []string{"install", "-e", "--id", "Amazon.AWSCLI"},
		}}, true
	}
	return nil, false
}

type callerJSON struct {
	UserID  string `json:"UserId"`
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
}

// Root returns the caller's current AWS account as a single node. If the caller
// has organizations:ListAccounts the full org listing would be richer, but most
// users are scoped to one account so we keep the default quiet.
func (a *AWS) Root(ctx context.Context) ([]provider.Node, error) {
	out, err := a.aws.Run(ctx, "sts", "get-caller-identity", "--output", "json")
	if err != nil {
		return nil, err
	}
	return parseCaller(out)
}

func parseCaller(data []byte) ([]provider.Node, error) {
	var c callerJSON
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse aws sts caller identity: %w", err)
	}
	if c.Account == "" {
		return nil, fmt.Errorf("aws: empty caller identity — run `aws configure sso` or `aws configure`")
	}
	return []provider.Node{{
		ID:    c.Account,
		Name:  c.Account,
		Kind:  provider.KindAccount,
		State: "Active",
		Meta: map[string]string{
			"arn":    c.Arn,
			"userId": c.UserID,
		},
	}}, nil
}

func (a *AWS) Children(ctx context.Context, parent provider.Node) ([]provider.Node, error) {
	switch parent.Kind {
	case provider.KindAccount:
		return a.regions(ctx, parent)
	case provider.KindRegion:
		return a.resources(ctx, parent)
	default:
		return nil, fmt.Errorf("aws: no children for kind %q", parent.Kind)
	}
}

type regionsJSON struct {
	Regions []struct {
		RegionName string `json:"RegionName"`
		Endpoint   string `json:"Endpoint"`
	} `json:"Regions"`
}

func (a *AWS) regions(ctx context.Context, account provider.Node) ([]provider.Node, error) {
	out, err := a.aws.Run(ctx, "ec2", "describe-regions", "--output", "json")
	if err != nil {
		return nil, err
	}
	return parseRegions(out, account)
}

func parseRegions(data []byte, account provider.Node) ([]provider.Node, error) {
	var envelope regionsJSON
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse aws describe-regions: %w", err)
	}
	nodes := make([]provider.Node, 0, len(envelope.Regions))
	parent := account
	for _, r := range envelope.Regions {
		nodes = append(nodes, provider.Node{
			ID:       r.RegionName,
			Name:     r.RegionName,
			Kind:     provider.KindRegion,
			Location: r.RegionName,
			State:    "available",
			Parent:   &parent,
			Meta: map[string]string{
				"endpoint":  r.Endpoint,
				"accountId": account.ID,
			},
		})
	}
	return nodes, nil
}

type resourcesJSON struct {
	ResourceTagMappingList []struct {
		ResourceARN string `json:"ResourceARN"`
	} `json:"ResourceTagMappingList"`
}

func (a *AWS) resources(ctx context.Context, region provider.Node) ([]provider.Node, error) {
	out, err := a.aws.Run(ctx,
		"resourcegroupstaggingapi", "get-resources",
		"--region", region.ID,
		"--output", "json",
	)
	if err != nil {
		return nil, err
	}
	return parseResources(out, region)
}

func parseResources(data []byte, region provider.Node) ([]provider.Node, error) {
	var envelope resourcesJSON
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse aws tagging get-resources: %w", err)
	}
	parent := region
	nodes := make([]provider.Node, 0, len(envelope.ResourceTagMappingList))
	for _, r := range envelope.ResourceTagMappingList {
		service := serviceFromARN(r.ResourceARN)
		restype := resourceTypeFromARN(r.ResourceARN)
		typeCol := service
		if restype != "" {
			typeCol = service + ":" + restype
		}
		nodes = append(nodes, provider.Node{
			ID:       r.ResourceARN,
			Name:     nameFromARN(r.ResourceARN),
			Kind:     provider.KindResource,
			Location: region.ID,
			State:    service,
			Parent:   &parent,
			Meta: map[string]string{
				"arn":    r.ResourceARN,
				"region": region.ID,
				"type":   typeCol,
			},
		})
	}
	return nodes, nil
}

// resourceTypeFromARN returns the resource-type segment from an ARN.
//
//	arn:aws:ec2:us-east-1:123:instance/i-abc      → instance
//	arn:aws:iam::123:role/my-role                 → role
//	arn:aws:lambda:us-east-1:123:function:f       → function
//	arn:aws:s3:::my-bucket                        → ""
func resourceTypeFromARN(arn string) string {
	// arn:aws:<service>:<region>:<account>:<rest>
	// rest may be:  <type>/<name>   OR  <type>:<name>   OR  <name>
	parts := 0
	start := 0
	for i := 0; i < len(arn); i++ {
		if arn[i] == ':' {
			parts++
			if parts == 5 {
				start = i + 1
				break
			}
		}
	}
	rest := arn[start:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' || rest[i] == ':' {
			return rest[:i]
		}
	}
	return ""
}

func (a *AWS) PortalURL(n provider.Node) string {
	switch n.Kind {
	case provider.KindAccount:
		return consoleHome
	case provider.KindRegion:
		return fmt.Sprintf("https://%s.console.aws.amazon.com/console/home?region=%s", n.ID, n.ID)
	case provider.KindResource:
		region := n.Meta["region"]
		if region == "" {
			return consoleHome
		}
		return fmt.Sprintf("https://%s.console.aws.amazon.com/console/home?region=%s", region, region)
	default:
		return consoleHome
	}
}

func (a *AWS) Details(ctx context.Context, n provider.Node) ([]byte, error) {
	switch n.Kind {
	case provider.KindAccount:
		return a.aws.Run(ctx, "sts", "get-caller-identity", "--output", "json")
	case provider.KindRegion:
		return []byte(fmt.Sprintf(`{"region": %q, "endpoint": %q}`, n.ID, n.Meta["endpoint"])), nil
	case provider.KindResource:
		return []byte(fmt.Sprintf(`{"arn": %q, "region": %q}`, n.Meta["arn"], n.Meta["region"])), nil
	default:
		return nil, fmt.Errorf("aws: no detail view for kind %q", n.Kind)
	}
}

// nameFromARN pulls the human-readable tail of an ARN.
//
//	arn:aws:ec2:us-east-1:123:instance/i-abc → i-abc
//	arn:aws:s3:::my-bucket                   → my-bucket
func nameFromARN(arn string) string {
	// take everything after the last / or :
	last := -1
	for i := 0; i < len(arn); i++ {
		if arn[i] == '/' || arn[i] == ':' {
			last = i
		}
	}
	if last >= 0 && last < len(arn)-1 {
		return arn[last+1:]
	}
	return arn
}

// serviceFromARN returns the service segment, e.g. "ec2", "s3", "lambda".
func serviceFromARN(arn string) string {
	// arn:aws:<service>:...
	if len(arn) < 8 || arn[:4] != "arn:" {
		return ""
	}
	rest := arn[4:]
	// skip partition
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			rest = rest[i+1:]
			break
		}
	}
	// service is next segment
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			return rest[:i]
		}
	}
	return ""
}
