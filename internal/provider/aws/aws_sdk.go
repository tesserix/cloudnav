package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/tesserix/cloudnav/internal/provider"
)

// loggedInSDK probes the active credentials by minting one
// GetCallerIdentity call. Mirrors the gcp/auth.go ADC check —
// returns nil when creds are usable end-to-end, propagates the
// SDK error otherwise so the caller can fall back to the CLI.
func (a *AWS) loggedInSDK(ctx context.Context) error {
	client, err := a.stsClient(ctx)
	if err != nil || client == nil {
		return err
	}
	_, err = client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	return err
}

// listOrgAccountsSDK returns active member accounts when the
// caller has organizations:ListAccounts. Returns (nil, false, err)
// on auth failure / standalone-account / not-the-management-account
// so the caller falls through to either the CLI path (also tolerant)
// or the GetCallerIdentity-only single-account fallback.
func (a *AWS) listOrgAccountsSDK(ctx context.Context) ([]provider.Node, bool, error) {
	client, err := a.orgsClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	pager := organizations.NewListAccountsPaginator(client, &organizations.ListAccountsInput{})
	out := make([]provider.Node, 0, 16)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, true, err
		}
		for _, acc := range page.Accounts {
			if acc.Status != orgtypes.AccountStatusActive {
				continue
			}
			id := aws.ToString(acc.Id)
			name := aws.ToString(acc.Name)
			if name == "" {
				name = id
			}
			meta := map[string]string{
				"accountId": id,
				"email":     aws.ToString(acc.Email),
				"source":    "sdk",
			}
			if acc.JoinedTimestamp != nil {
				meta["createdTime"] = acc.JoinedTimestamp.Format("2006-01-02T15:04:05Z")
			}
			out = append(out, provider.Node{
				ID:    id,
				Name:  name,
				Kind:  provider.KindAccount,
				State: string(acc.Status),
				Meta:  meta,
			})
		}
	}
	return out, true, nil
}

// formatTagsMap renders a tags map as the same compact "k=v, k=v"
// string formatAWSTags produces from the JSON struct shape. Kept
// here so the SDK path doesn't have to fake the JSON struct shape
// that formatAWSTags was written against.
func formatTagsMap(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		if k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	// sort.Strings is fine here; alphabetical keeps the column
	// rendering deterministic across runs.
	sortStrings(keys)
	var b []byte
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',', ' ')
		}
		b = append(b, k...)
		if v := tags[k]; v != "" {
			b = append(b, '=')
			b = append(b, v...)
		}
	}
	return string(b)
}

// sortStrings is the local insertion sort — N is tiny (resource
// tag count) so we avoid pulling sort.Strings just for this file.
func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j-1] > in[j]; j-- {
			in[j-1], in[j] = in[j], in[j-1]
		}
	}
}

// regionsSDK returns every available region for an account via
// ec2:DescribeRegions. Same shape as parseRegions(); falls back to
// the CLI on SDK auth failure.
func (a *AWS) regionsSDK(ctx context.Context, account provider.Node) ([]provider.Node, bool, error) {
	client, err := a.ec2Client(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	out, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, true, err
	}
	parent := account
	nodes := make([]provider.Node, 0, len(out.Regions))
	for _, r := range out.Regions {
		nodes = append(nodes, provider.Node{
			ID:       aws.ToString(r.RegionName),
			Name:     aws.ToString(r.RegionName),
			Kind:     provider.KindRegion,
			Location: aws.ToString(r.RegionName),
			State:    "available",
			Parent:   &parent,
			Meta: map[string]string{
				"endpoint":  aws.ToString(r.Endpoint),
				"accountId": account.ID,
				"source":    "sdk",
			},
		})
	}
	return nodes, true, nil
}

// resourcesSDK returns every tagged resource in a region via
// resourcegroupstaggingapi:GetResources, paginated. Mirrors
// parseResources() shape so the TUI doesn't notice which path
// produced the data.
func (a *AWS) resourcesSDK(ctx context.Context, region provider.Node) ([]provider.Node, bool, error) {
	client, err := a.taggingClientForRegion(ctx, region.ID)
	if err != nil || client == nil {
		return nil, false, err
	}
	pager := resourcegroupstaggingapi.NewGetResourcesPaginator(client, &resourcegroupstaggingapi.GetResourcesInput{})
	parent := region
	nodes := make([]provider.Node, 0, 64)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, true, err
		}
		for _, r := range page.ResourceTagMappingList {
			arn := aws.ToString(r.ResourceARN)
			service := serviceFromARN(arn)
			restype := resourceTypeFromARN(arn)
			typeCol := service
			if restype != "" {
				typeCol = service + ":" + restype
			}
			meta := map[string]string{
				"arn":    arn,
				"region": region.ID,
				"type":   typeCol,
				"source": "sdk",
			}
			tags := make(map[string]string, len(r.Tags))
			for _, t := range r.Tags {
				tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			if tagsStr := formatTagsMap(tags); tagsStr != "" {
				meta["tags"] = tagsStr
			}
			nodes = append(nodes, provider.Node{
				ID:       arn,
				Name:     nameFromARN(arn),
				Kind:     provider.KindResource,
				Location: region.ID,
				State:    service,
				Parent:   &parent,
				Meta:     meta,
			})
		}
	}
	return nodes, true, nil
}

// callerIdentitySDK returns a single-account Node from
// sts:GetCallerIdentity. Used as the fallback when
// organizations:ListAccounts isn't permitted (standalone accounts,
// non-management accounts).
func (a *AWS) callerIdentitySDK(ctx context.Context) (provider.Node, bool, error) {
	client, err := a.stsClient(ctx)
	if err != nil || client == nil {
		return provider.Node{}, false, err
	}
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return provider.Node{}, true, err
	}
	id := aws.ToString(out.Account)
	if id == "" {
		return provider.Node{}, true, fmt.Errorf("aws sts: empty account id")
	}
	return provider.Node{
		ID:   id,
		Name: id,
		Kind: provider.KindAccount,
		Meta: map[string]string{
			"accountId": id,
			"arn":       aws.ToString(out.Arn),
			"userId":    aws.ToString(out.UserId),
			"source":    "sdk",
		},
	}, true, nil
}
