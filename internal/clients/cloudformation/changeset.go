// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	sdktypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
)

const (
	// The change set name will be formatted as "flux-<generation sequence number>".
	fmtChangeSetName       = "flux-%d-%s"
	maxLengthChangeSetName = 128
)

var (
	// all except alphanumeric characters
	changeSetNameSpecialChars, _ = regexp.Compile("[^a-zA-Z0-9]+")
)

type changeSet struct {
	name      string
	stackName string
	region    string
	csType    sdktypes.ChangeSetType
	client    changeSetAPI
	ctx       context.Context
}

// GetChangeSetName generates a unique change set name using the generation number
// (a specific version of the CloudFormationStack Spec contents) and the source
// revision (such as the branch and commit ID for git sources).
//
// Examples:
//
//	Git repository: main@sha1:132f4e719209eb10b9485302f8593fc0e680f4fc
//	Bucket: sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
//	OCI repository: latest@sha256:3b6cdcc7adcc9a84d3214ee1c029543789d90b5ae69debe9efa3f66e982875de
func GetChangeSetName(generation int64, sourceRevision string) string {
	name := fmt.Sprintf(fmtChangeSetName, generation, sourceRevision)
	name = changeSetNameSpecialChars.ReplaceAllString(name, "-")
	if len(name) <= maxLengthChangeSetName {
		return name
	}
	return name[:maxLengthChangeSetName]
}

// ExtractChangeSetName extracts the name of the change set from the change set ARN
// Example:
// arn:aws:cloudformation:us-west-2:123456789012:changeSet/<name>/<uuid> -> name
func ExtractChangeSetName(arn string) string {
	arnParts := strings.Split(arn, "/")
	if len(arnParts) < 2 {
		return ""
	}
	return arnParts[1]
}

func newCreateChangeSet(ctx context.Context, cfnClient changeSetAPI, region string, stackName string, generation int64, sourceRevision string) *changeSet {
	return &changeSet{
		name:      GetChangeSetName(generation, sourceRevision),
		stackName: stackName,
		region:    region,
		csType:    sdktypes.ChangeSetTypeCreate,
		client:    cfnClient,
		ctx:       ctx,
	}
}

func newUpdateChangeSet(ctx context.Context, cfnClient changeSetAPI, region string, stackName string, generation int64, sourceRevision string) *changeSet {
	return &changeSet{
		name:      GetChangeSetName(generation, sourceRevision),
		stackName: stackName,
		region:    region,
		csType:    sdktypes.ChangeSetTypeUpdate,
		client:    cfnClient,
		ctx:       ctx,
	}
}

func (cs *changeSet) String() string {
	return fmt.Sprintf("change set %s for stack %s", cs.name, cs.stackName)
}

// create creates a ChangeSet, waits until it's created, and returns the change set ARN on success.
func (cs *changeSet) create(conf *types.StackConfig) (string, error) {
	input := &cloudformation.CreateChangeSetInput{
		ChangeSetName:       aws.String(cs.name),
		StackName:           aws.String(cs.stackName),
		Description:         aws.String("Managed by Flux"),
		ChangeSetType:       cs.csType,
		TemplateURL:         aws.String(conf.TemplateURL),
		Parameters:          conf.Parameters,
		Tags:                conf.Tags,
		IncludeNestedStacks: aws.Bool(true),
		Capabilities: []sdktypes.Capability{
			sdktypes.CapabilityCapabilityIam,
			sdktypes.CapabilityCapabilityNamedIam,
			sdktypes.CapabilityCapabilityAutoExpand,
		},
	}

	opts := func(opts *cloudformation.Options) {
		if cs.region != "" {
			opts.Region = cs.region
		}
	}

	out, err := cs.client.CreateChangeSet(cs.ctx, input, opts)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", cs, err)
	}
	return *out.Id, nil
}

// describe collects all the changes and statuses that the change set will apply and returns them.
func (cs *changeSet) describe() (*types.ChangeSetDescription, error) {
	var arn string
	var status sdktypes.ChangeSetStatus
	var executionStatus sdktypes.ExecutionStatus
	var statusReason string
	var changes []sdktypes.Change
	var nextToken *string
	for {
		out, err := cs.client.DescribeChangeSet(cs.ctx, &cloudformation.DescribeChangeSetInput{
			ChangeSetName: aws.String(cs.name),
			StackName:     aws.String(cs.stackName),
			NextToken:     nextToken,
		}, func(opts *cloudformation.Options) {
			if cs.region != "" {
				opts.Region = cs.region
			}
		})
		if err != nil {
			return nil, fmt.Errorf("describe %s: %w", cs, err)
		}
		arn = *out.ChangeSetId
		status = out.Status
		executionStatus = out.ExecutionStatus
		if out.StatusReason != nil {
			statusReason = *out.StatusReason
		}
		changes = append(changes, out.Changes...)
		nextToken = out.NextToken

		if nextToken == nil { // no more results left
			break
		}
	}
	return &types.ChangeSetDescription{
		Arn:             arn,
		Status:          status,
		ExecutionStatus: executionStatus,
		StatusReason:    statusReason,
		Changes:         changes,
	}, nil
}

// execute executes a created change set.
func (cs *changeSet) execute() error {
	_, err := cs.client.ExecuteChangeSet(cs.ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName: aws.String(cs.name),
		StackName:     aws.String(cs.stackName),
	}, func(opts *cloudformation.Options) {
		if cs.region != "" {
			opts.Region = cs.region
		}
	})
	if err != nil {
		return fmt.Errorf("execute %s: %w", cs, err)
	}
	return nil
}

// delete removes the change set.
func (cs *changeSet) delete() error {
	_, err := cs.client.DeleteChangeSet(cs.ctx, &cloudformation.DeleteChangeSetInput{
		ChangeSetName: aws.String(cs.name),
		StackName:     aws.String(cs.stackName),
	}, func(opts *cloudformation.Options) {
		if cs.region != "" {
			opts.Region = cs.region
		}
	})
	if err != nil {
		return fmt.Errorf("delete %s: %w", cs, err)
	}
	return nil
}
