package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/tesserix/cloudnav/internal/provider"
)

const (
	awsServiceEC2 = "ec2"
	awsServiceS3  = "s3"
)

// Delete satisfies provider.Deleter for AWS. AWS doesn't have a
// global "delete this resource" RPC the way ARM does; each service
// has its own API. We dispatch on the ARN's service segment and
// route to the appropriate SDK client. Anything we don't handle
// returns ErrNotSupported with a portal hand-off hint — same shape
// as the GCP per-asset-type dispatcher.
func (a *AWS) Delete(ctx context.Context, n provider.Node) error {
	if n.Kind != provider.KindResource {
		return fmt.Errorf("%w: aws cannot delete kind %q (only individual resources)",
			provider.ErrNotSupported, n.Kind)
	}
	arn := n.ID
	service := serviceFromARN(arn)
	region := n.Meta["region"]
	if region == "" {
		region = regionFromARN(arn)
	}

	switch service {
	case awsServiceEC2:
		// Only instance ARNs flow through here today —
		// volumes / snapshots / security groups are listed in
		// the resource view too but cloudnav defers their
		// deletion to the portal until users explicitly ask
		// for it.
		if !strings.Contains(arn, ":instance/") {
			return fmt.Errorf("%w: aws delete for ec2 type %q (open the portal to delete this resource)",
				provider.ErrNotSupported, arn)
		}
		return a.deleteEC2Instance(ctx, region, lastSegmentARN(arn, "instance/"))
	case awsServiceS3:
		// S3 bucket names are global — no region needed for
		// the API call (the bucket's actual location is
		// resolved server-side).
		return a.deleteS3Bucket(ctx, n.Name)
	}
	return fmt.Errorf("%w: aws delete for service %q (open the portal to delete this resource)",
		provider.ErrNotSupported, service)
}

// deleteEC2Instance terminates an instance via ec2:TerminateInstances.
// Note: this is a hard terminate, not a stop — matches the
// "delete" verb's semantic across clouds.
func (a *AWS) deleteEC2Instance(ctx context.Context, region, instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("aws delete ec2 instance: missing instance id")
	}
	client, err := a.ec2ClientForRegion(ctx, region)
	if err != nil || client == nil {
		// Fall back to the AWS CLI when SDK isn't usable.
		_, err := a.aws.Run(ctx,
			"ec2", "terminate-instances",
			"--instance-ids", instanceID,
			"--region", region,
		)
		return err
	}
	_, err = client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	})
	return err
}

// deleteS3Bucket calls s3:DeleteBucket. Empty-bucket requirement
// is AWS's, not ours — the SDK returns the typed error.
func (a *AWS) deleteS3Bucket(ctx context.Context, name string) error {
	cfg, err := sdkConfig(ctx)
	if err != nil {
		_, err := a.aws.Run(ctx, "s3api", "delete-bucket", "--bucket", name)
		return err
	}
	client := s3.NewFromConfig(cfg)
	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// regionFromARN pulls the region segment from a fully-qualified ARN.
//
//	arn:aws:ec2:us-east-1:123:instance/i-abc → us-east-1
func regionFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// lastSegmentARN returns the segment of an ARN after a known marker.
// Used to extract instance ids etc. Returns "" when the marker is
// absent so the caller can fail with a clear message.
func lastSegmentARN(arn, marker string) string {
	idx := strings.Index(arn, marker)
	if idx < 0 {
		return ""
	}
	return arn[idx+len(marker):]
}

// Compile-time assert AWS satisfies Deleter.
var _ provider.Deleter = (*AWS)(nil)
