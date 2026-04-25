package aws

import (
	"context"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Identity satisfies provider.Identifier. Probes the standard SDK
// auth chain and reports both the IAM ARN and the credential source
// that resolved.
//
// SDK chain (config.LoadDefaultConfig):
//
//  1. Environment vars: AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
//     (+ AWS_SESSION_TOKEN for assumed-role); OR
//     AWS_WEB_IDENTITY_TOKEN_FILE + AWS_ROLE_ARN (OIDC / IRSA).
//  2. Shared credentials file (~/.aws/credentials, profile-based).
//  3. Shared config file (~/.aws/config, including SSO profiles).
//  4. ECS task role (AWS_CONTAINER_CREDENTIALS_RELATIVE_URI).
//  5. EC2 instance metadata (IMDSv2).
func (a *AWS) Identity(ctx context.Context) (provider.Identity, error) {
	method := detectAWSMethod()
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client, err := a.stsClient(c)
	if err != nil || client == nil {
		return provider.Identity{Method: method}, err
	}
	out, err := client.GetCallerIdentity(c, &sts.GetCallerIdentityInput{})
	if err != nil {
		return provider.Identity{Method: method}, err
	}
	return provider.Identity{
		Who:    aws.ToString(out.Arn),
		Method: method,
	}, nil
}

// detectAWSMethod sniffs the same env signals
// config.LoadDefaultConfig consults, in chain order. First match
// wins — same precedence as the SDK so the label matches actual
// behaviour.
func detectAWSMethod() string {
	switch {
	case os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" && os.Getenv("AWS_ROLE_ARN") != "":
		return "Web Identity / OIDC (IRSA, GitHub Actions, etc.)"
	case os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SESSION_TOKEN") != "":
		return "Temporary credentials (env vars)"
	case os.Getenv("AWS_ACCESS_KEY_ID") != "":
		return "Static IAM keys (AWS_ACCESS_KEY_ID env vars)"
	case os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" ||
		os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "":
		return "ECS task role (container metadata)"
	case os.Getenv("AWS_PROFILE") != "":
		return "Shared profile: " + os.Getenv("AWS_PROFILE") + " (~/.aws/config)"
	default:
		return "Default profile / SSO (~/.aws/credentials)"
	}
}
