// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

// Package cloudformation provides a client to make API requests to AWS CloudFormation.
package cloudformation

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go/middleware"
)

// CloudFormation represents a client to make requests to AWS CloudFormation.
type CloudFormation struct {
	client
	ctx context.Context
}

// New creates a new CloudFormation client.
func New(ctx context.Context) (*CloudFormation, error) {
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithAPIOptions([]func(*middleware.Stack) error{
			awsmiddleware.AddUserAgentKey("cfn-flux-controller"),
		}),
	)
	if err != nil {
		return nil, err
	}

	return &CloudFormation{
		client: cloudformation.NewFromConfig(cfg),
		ctx:    ctx,
	}, nil
}

// Describe returns a description of an existing stack.
// If the stack does not exist, returns ErrStackNotFound.
func (c *CloudFormation) DescribeStack(stack *Stack) (*StackDescription, error) {
	out, err := c.client.DescribeStacks(c.ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stack.Name),
	}, func(opts *cloudformation.Options) {
		if stack.Region != "" {
			opts.Region = stack.Region
		}
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return nil, &ErrStackNotFound{name: stack.Name}
		}
		return nil, fmt.Errorf("describe stack %s: %w", stack.Name, err)
	}
	if len(out.Stacks) == 0 {
		return nil, &ErrStackNotFound{name: stack.Name}
	}
	if out.Stacks[0].StackStatus == types.StackStatusReviewInProgress {
		// there is a creation change set for the stack, but it has not been executed,
		// so the stack has not been created yet
		return nil, &ErrStackNotFound{name: stack.Name}
	}
	if out.Stacks[0].StackStatus == types.StackStatusDeleteComplete {
		// the stack was previously successfully deleted
		return nil, &ErrStackNotFound{name: stack.Name}
	}
	descr := StackDescription(out.Stacks[0])
	return &descr, nil
}

// DescribeChangeSet gathers and returns all changes for the stack's current change set.
// If the stack or changeset does not exist, returns ErrChangeSetNotFound.
func (c *CloudFormation) DescribeChangeSet(stack *Stack) (*ChangeSetDescription, error) {
	var changeSetName string
	if stack.ChangeSetArn != "" {
		changeSetName = stack.ChangeSetArn
	} else {
		changeSetName = GetChangeSetName(stack.Generation, stack.SourceRevision)
	}
	cs := &changeSet{name: changeSetName, stackName: stack.Name, region: stack.Region, client: c.client, ctx: c.ctx}

	out, err := cs.describe()
	if err != nil {
		if changeSetDoesNotExist(err) {
			return nil, &ErrChangeSetNotFound{name: changeSetName, stackName: stack.Name}
		}
		return nil, err
	}

	stack.ChangeSetArn = out.Arn

	if out.IsDeleted() {
		return nil, &ErrChangeSetNotFound{name: changeSetName, stackName: stack.Name}
	}

	// The change set was empty. The status reason will be like
	// "The submitted information didn't contain changes. Submit different information to create a change set."
	if out.IsEmpty() {
		return nil, &ErrChangeSetEmpty{name: changeSetName, stackName: stack.Name, Arn: out.Arn}
	}

	return out, nil
}

// CreateStack begins the process of deploying a new CloudFormation stack by creating a change set.
// The change set must be executed when it is successfully created.
func (c *CloudFormation) CreateStack(stack *Stack) (changeSetArn string, err error) {
	cs, err := newCreateChangeSet(c.ctx, c.client, stack.Region, stack.Name, stack.Generation, stack.SourceRevision)
	if err != nil {
		return "", err
	}
	arn, err := cs.create(stack.StackConfig)
	if err != nil {
		return "", err
	}
	stack.ChangeSetArn = arn
	return arn, nil
}

// UpdateStack begins the process of updating an existing CloudFormation stack with new configuration
// by creating a change set.
// The change set must be executed when it is successfully created.
func (c *CloudFormation) UpdateStack(stack *Stack) (changeSetArn string, err error) {
	cs, err := newUpdateChangeSet(c.ctx, c.client, stack.Region, stack.Name, stack.Generation, stack.SourceRevision)
	if err != nil {
		return "", err
	}
	arn, err := cs.create(stack.StackConfig)
	if err != nil {
		return "", err
	}
	stack.ChangeSetArn = arn
	return arn, nil
}

// ExecutChangeSet starts the execution of the stack's current change set.
// If the stack or changeset does not exist, returns ErrChangeSetNotFound.
func (c *CloudFormation) ExecuteChangeSet(stack *Stack) error {
	cs := &changeSet{name: stack.ChangeSetArn, stackName: stack.Name, region: stack.Region, client: c.client, ctx: c.ctx}
	return cs.execute()
}

// Delete removes an existing CloudFormation stack.
// If the stack doesn't exist then do nothing.
func (c *CloudFormation) DeleteStack(stack *Stack) error {
	_, err := c.client.DeleteStack(c.ctx, &cloudformation.DeleteStackInput{
		StackName: aws.String(stack.Name),
	}, func(opts *cloudformation.Options) {
		if stack.Region != "" {
			opts.Region = stack.Region
		}
	})
	if err != nil {
		if !stackDoesNotExist(err) {
			return fmt.Errorf("delete stack %s: %w", stack.Name, err)
		}
		// Move on if stack is already deleted.
	}
	return nil
}

// Delete removes an existing CloudFormation change set.
// If the change set doesn't exist then do nothing.
func (c *CloudFormation) DeleteChangeSet(stack *Stack) error {
	cs := &changeSet{name: stack.ChangeSetArn, stackName: stack.Name, region: stack.Region, client: c.client, ctx: c.ctx}
	if err := cs.delete(); err != nil {
		if !changeSetDoesNotExist(err) {
			return err
		}
		// Move on if change set is already deleted.
	}
	return nil
}

// ContinueRollback attempts to continue an Update rollback for an existing CloudFormation stack.
func (c *CloudFormation) ContinueRollback(stack *Stack) error {
	_, err := c.client.ContinueUpdateRollback(c.ctx, &cloudformation.ContinueUpdateRollbackInput{
		StackName: aws.String(stack.Name),
	}, func(opts *cloudformation.Options) {
		if stack.Region != "" {
			opts.Region = stack.Region
		}
	})
	return err
}
