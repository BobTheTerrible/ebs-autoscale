package ebs_autoscale

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"io"
)

type Ec2Host struct {
	InstanceId       string
	InstanceArn      string
	AvailabilityZone string
	Region           string
	Tags             []types.Tag
}

func NewEc2Host(ctx context.Context) (*Ec2Host, error) {

	// We do not need to know the Region for imds calls
	imdsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	imdsClient := imds.NewFromConfig(imdsCfg)
	_, err = GetAWSEc2Metadata(ctx, "instance-id", *imdsClient)
	if err != nil {
		return nil, err
	}

	instanceId, err := GetAWSEc2Metadata(ctx, "instance-id", *imdsClient)
	if err != nil {
		return nil, err
	}
	availabilityZone, err := GetAWSEc2Metadata(ctx, "placement/availability-zone", *imdsClient)
	if err != nil {
		return nil, err
	}

	// We can use this to set the Region of the AWS clients.
	region := availabilityZone[:len(availabilityZone)-1]

	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithDefaultRegion(region))
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(awsConfig)
	stsClient := sts.NewFromConfig(awsConfig)

	// This is a bit of a hack because there is no way to fetch the actual arn
	// We will use this arn externally to limit the attach/detach actions we can perform on a volume
	// see https://github.com/awslabs/amazon-ebs-autoscale/issues/28
	//arn:aws:ec2:<Region>:<account-number>:instance/<instance-id>
	callerID, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, err
	}

	instanceArn := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", region, *callerID.Account, instanceId)
	tagsOutput, err := ec2Client.DescribeTags(ctx, &ec2.DescribeTagsInput{
		Filters: []types.Filter{
			{
				Name: aws.String("resource-id"),
				Values: []string{
					instanceId,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	e := Ec2Host{
		InstanceId:       instanceId,
		InstanceArn:      instanceArn,
		AvailabilityZone: availabilityZone,
		Region:           region,
		Tags: func(tags []types.TagDescription) []types.Tag {
			volumeTags := make([]types.Tag, 0)
			for _, t := range tags {
				volumeTags = append(volumeTags,
					types.Tag{
						Key:   t.Key,
						Value: t.Value,
					},
				)
			}
			return volumeTags
		}(tagsOutput.Tags),
	}

	return &e, nil
}

// GetAWSEc2Metadata get EC2 instance metadata using
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/feature/ec2/imds#Client.GetMetadata
func GetAWSEc2Metadata(ctx context.Context, path string, client imds.Client) (value string, err error) {
	output, err := client.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: path,
	})
	if err != nil {
		return "", err
	}
	defer output.Content.Close() //nolint:errcheck
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", err
	}
	resp := string(bytes)
	return resp, err
}
