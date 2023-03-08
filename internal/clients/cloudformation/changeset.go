// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

const (
	// The change set name will be formatted as "flux-<generation sequence number>".
	fmtChangeSetName = "flux-%d"

	// Status reasons that can occur if the change set execution status is "FAILED".
	noChangesReason = "NO_CHANGES_REASON"
	noUpdatesReason = "NO_UPDATES_REASON"
)

var (
	inProgressChangeSetStatuses = []types.ChangeSetStatus{
		types.ChangeSetStatusCreateInProgress,
		types.ChangeSetStatusCreatePending,
		types.ChangeSetStatusDeleteInProgress,
		types.ChangeSetStatusDeletePending,
	}

	failedChangeSetStatuses = []types.ChangeSetStatus{
		types.ChangeSetStatusDeleteFailed,
		types.ChangeSetStatusFailed,
	}

	inProgressChangeSetExecutionStatuses = []types.ExecutionStatus{
		types.ExecutionStatusExecuteInProgress,
		types.ExecutionStatusUnavailable,
	}

	failedChangeSetExecutionStatuses = []types.ExecutionStatus{
		types.ExecutionStatusExecuteFailed,
		types.ExecutionStatusObsolete,
	}
)

// ChangeSetDescription is the output of the DescribeChangeSet action.
type ChangeSetDescription struct {
	Arn             string
	Status          types.ChangeSetStatus
	ExecutionStatus types.ExecutionStatus
	StatusReason    string
	CreationTime    time.Time
	Changes         []types.Change
}

type changeSet struct {
	name      string
	stackName string
	region    string
	csType    types.ChangeSetType
	client    changeSetAPI
	ctx       context.Context
}

func getChangeSetId(generation int64) string {
	// TODO Determine whether this is enough uniqueness:
	// Does the generation change when the source revision changes?
	// Does the generation change when the status changes?
	return fmt.Sprintf(fmtChangeSetName, generation)
}

func newCreateChangeSet(ctx context.Context, cfnClient changeSetAPI, region string, stackName string, generation int64) (*changeSet, error) {
	return &changeSet{
		name:      getChangeSetId(generation),
		stackName: stackName,
		region:    region,
		csType:    types.ChangeSetTypeCreate,
		client:    cfnClient,
		ctx:       ctx,
	}, nil
}

func newUpdateChangeSet(ctx context.Context, cfnClient changeSetAPI, region string, stackName string, generation int64) (*changeSet, error) {
	return &changeSet{
		name:      getChangeSetId(generation),
		stackName: stackName,
		region:    region,
		csType:    types.ChangeSetTypeUpdate,
		client:    cfnClient,
		ctx:       ctx,
	}, nil
}

func (cs *changeSet) String() string {
	return fmt.Sprintf("change set %s for stack %s", cs.name, cs.stackName)
}

// create creates a ChangeSet, waits until it's created, and returns the change set ARN on success.
func (cs *changeSet) create(conf *stackConfig) (string, error) {
	input := &cloudformation.CreateChangeSetInput{
		ChangeSetName:       aws.String(cs.name),
		StackName:           aws.String(cs.stackName),
		ChangeSetType:       cs.csType,
		Parameters:          conf.Parameters,
		Tags:                conf.Tags,
		IncludeNestedStacks: aws.Bool(true),
		Capabilities: []types.Capability{
			types.CapabilityCapabilityIam,
			types.CapabilityCapabilityNamedIam,
			types.CapabilityCapabilityAutoExpand,
		},
	}
	if conf.TemplateBody != "" {
		input.TemplateBody = aws.String(conf.TemplateBody)
	}
	if conf.TemplateURL != "" {
		input.TemplateURL = aws.String(conf.TemplateURL)
	}

	opts := func(opts *cloudformation.Options) {
		opts.Region = cs.region
	}

	out, err := cs.client.CreateChangeSet(cs.ctx, input, opts)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", cs, err)
	}
	return *out.Id, nil
}

// describe collects all the changes and statuses that the change set will apply and returns them.
func (cs *changeSet) describe() (*ChangeSetDescription, error) {
	var arn string
	var status types.ChangeSetStatus
	var executionStatus types.ExecutionStatus
	var statusReason string
	var creationTime time.Time
	var changes []types.Change
	var nextToken *string
	for {
		out, err := cs.client.DescribeChangeSet(cs.ctx, &cloudformation.DescribeChangeSetInput{
			ChangeSetName: aws.String(cs.name),
			StackName:     aws.String(cs.stackName),
			NextToken:     nextToken,
		}, func(opts *cloudformation.Options) {
			opts.Region = cs.region
		})
		if err != nil {
			return nil, fmt.Errorf("describe %s: %w", cs, err)
		}
		arn = *out.ChangeSetId
		status = out.Status
		executionStatus = out.ExecutionStatus
		statusReason = *out.StatusReason
		creationTime = *out.CreationTime
		changes = append(changes, out.Changes...)
		nextToken = out.NextToken

		if nextToken == nil { // no more results left
			break
		}
	}
	return &ChangeSetDescription{
		Arn:             arn,
		Status:          status,
		ExecutionStatus: executionStatus,
		StatusReason:    statusReason,
		CreationTime:    creationTime,
		Changes:         changes,
	}, nil
}

// execute executes a created change set.
func (cs *changeSet) execute() error {
	_, err := cs.client.ExecuteChangeSet(cs.ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName: aws.String(cs.name),
		StackName:     aws.String(cs.stackName),
	}, func(opts *cloudformation.Options) {
		opts.Region = cs.region
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
		opts.Region = cs.region
	})
	if err != nil {
		return fmt.Errorf("delete %s: %w", cs, err)
	}
	return nil
}

func (d *ChangeSetDescription) IsEmpty() bool {
	return (len(d.Changes) == 0 && strings.Contains(d.StatusReason, "didn't contain changes")) ||
		d.StatusReason == noChangesReason ||
		d.StatusReason == noUpdatesReason
}

func (d *ChangeSetDescription) IsDeleted() bool {
	return d.Status == types.ChangeSetStatusDeleteComplete
}

func (d *ChangeSetDescription) IsCreated() bool {
	return d.Status == types.ChangeSetStatusCreateComplete
}

func (d *ChangeSetDescription) InProgress() bool {
	return changesetStatusListContains(d.Status, inProgressChangeSetStatuses) ||
		changesetExecutionStatusListContains(d.ExecutionStatus, inProgressChangeSetExecutionStatuses)
}

func (d *ChangeSetDescription) IsFailed() bool {
	return changesetStatusListContains(d.Status, failedChangeSetStatuses) ||
		changesetExecutionStatusListContains(d.ExecutionStatus, failedChangeSetExecutionStatuses)
}

func (d *ChangeSetDescription) IsSuccess() bool {
	return d.IsCreated() && d.ExecutionStatus == types.ExecutionStatusExecuteComplete
}

func (d *ChangeSetDescription) ReadyForExecution() bool {
	return d.IsCreated() && d.ExecutionStatus == types.ExecutionStatusAvailable
}

func changesetStatusListContains(element types.ChangeSetStatus, statusList []types.ChangeSetStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}

func changesetExecutionStatusListContains(element types.ExecutionStatus, statusList []types.ExecutionStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}
