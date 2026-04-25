// AWS SDK foundation. Mirrors internal/provider/gcp/sdk_client.go —
// lazily-initialised SDK clients with cached errors so a failed
// auth probe doesn't keep re-paying latency. Every SDK method has a
// CLI fallback (the existing a.aws.Run path) so cloudnav stays
// usable on hosts where the SDK can't auth (no ~/.aws/config, broken
// IMDS, etc.).
package aws

import (
	"context"
	"sync"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// sdkConfig holds the lazy-initialised aws.Config the rest of the
// SDK clients build on. Resolves credentials in this order (per
// the SDK's standard chain):
//
//  1. AWS_PROFILE / AWS_ACCESS_KEY_ID env vars.
//  2. ~/.aws/credentials (static keys).
//  3. ~/.aws/config + active SSO session.
//  4. EC2 IMDS / ECS task role (when running on AWS infra).
//
// The active region comes from AWS_REGION → ~/.aws/config →
// us-east-1 default. cloudnav doesn't override the region — it's
// the same source aws-cli reads — so users can switch regions
// with the same tooling they already use.
var (
	sdkCfgOnce    sync.Once
	sdkCfgValue   awsv2.Config
	sdkCfgInitErr error
)

func sdkConfig(ctx context.Context) (awsv2.Config, error) {
	sdkCfgOnce.Do(func() {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cfg, err := config.LoadDefaultConfig(c)
		if err != nil {
			sdkCfgInitErr = err
			return
		}
		sdkCfgValue = cfg
	})
	return sdkCfgValue, sdkCfgInitErr
}

// Per-service client singletons. Each client is keyed off the same
// shared sdkConfig so creds + region propagate consistently. Region-
// specific clients (ec2 across multiple regions, e.g. when listing
// instances in every region) are built on demand from sdkConfig
// with a region override; that's why this struct holds only the
// "default region" version.
var (
	stsOnce    sync.Once
	stsClient  *sts.Client
	stsInitErr error
)

func (a *AWS) stsClient(ctx context.Context) (*sts.Client, error) {
	stsOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			stsInitErr = err
			return
		}
		stsClient = sts.NewFromConfig(cfg)
	})
	return stsClient, stsInitErr
}

var (
	orgsOnce    sync.Once
	orgsClient  *organizations.Client
	orgsInitErr error
)

func (a *AWS) orgsClient(ctx context.Context) (*organizations.Client, error) {
	orgsOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			orgsInitErr = err
			return
		}
		orgsClient = organizations.NewFromConfig(cfg)
	})
	return orgsClient, orgsInitErr
}

var (
	ec2DefaultOnce    sync.Once
	ec2DefaultClient  *ec2.Client
	ec2DefaultInitErr error
)

// ec2Client returns the EC2 client scoped to the SDK's default
// region. For cross-region calls (DescribeInstances in every
// account region) we build per-region clients via ec2ClientForRegion.
func (a *AWS) ec2Client(ctx context.Context) (*ec2.Client, error) {
	ec2DefaultOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			ec2DefaultInitErr = err
			return
		}
		ec2DefaultClient = ec2.NewFromConfig(cfg)
	})
	return ec2DefaultClient, ec2DefaultInitErr
}

// ec2ClientForRegion builds a region-pinned EC2 client. Cheap —
// the underlying HTTP transport is shared with the default client
// via the SDK's smithy stack.
func (a *AWS) ec2ClientForRegion(ctx context.Context, region string) (*ec2.Client, error) {
	cfg, err := sdkConfig(ctx)
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		o.Region = region
	}), nil
}

// taggingClientForRegion builds a region-pinned Resource Groups
// Tagging API client. Used by the resource drill — every region
// has its own tagging endpoint, and we don't share a default-
// region client because the resource drill never targets the
// default region implicitly.
func (a *AWS) taggingClientForRegion(ctx context.Context, region string) (*resourcegroupstaggingapi.Client, error) {
	cfg, err := sdkConfig(ctx)
	if err != nil {
		return nil, err
	}
	return resourcegroupstaggingapi.NewFromConfig(cfg, func(o *resourcegroupstaggingapi.Options) {
		o.Region = region
	}), nil
}
