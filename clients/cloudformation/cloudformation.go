// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

// Package cloudformation provides a client to make API requests to AWS CloudFormation.
package cloudformation

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
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

// Create deploys a new CloudFormation stack using Change Sets.
// If the stack already exists in a failed state, deletes the stack and re-creates it.
func (c *CloudFormation) Create(stack *Stack) (changeSetID string, err error) {
	descr, err := c.Describe(stack)
	if err != nil {
		var stackNotFound *ErrStackNotFound
		if !errors.As(err, &stackNotFound) {
			return "", err
		}
		// If the stack does not exist, create it.
		return c.create(stack)
	}
	status := StackStatus(string(descr.StackStatus))
	// TODO CLARE: handle cleaning up a stack that exists but previously failed to create
	/*
		if status.requiresCleanup() {
			// If the stack exists, but failed to create, we'll clean it up and then re-create it.
			if err := c.DeleteAndWait(stack.Name); err != nil {
				return "", fmt.Errorf("clean up previously failed stack %s: %w", stack.Name, err)
			}
			return c.create(stack)
		}
	*/
	if status.InProgress() {
		return "", &ErrStackUpdateInProgress{
			Name: stack.Name,
		}
	}
	return "", &ErrStackAlreadyExists{
		Name:  stack.Name,
		Stack: descr,
	}
}

// DescribeChangeSet gathers and returns all changes for a change set.
func (c *CloudFormation) DescribeChangeSet(changeSetID string, stack *Stack) (*ChangeSetDescription, error) {
	cs := &changeSet{name: changeSetID, stackName: stack.Name, region: stack.Region, client: c.client}
	out, err := cs.describe()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update updates an existing CloudFormation with the new configuration.
// If there are no changes for the stack, deletes the empty change set and returns ErrChangeSetEmpty.
func (c *CloudFormation) Update(stack *Stack) (changeSetID string, err error) {
	descr, err := c.Describe(stack)
	if err != nil {
		return "", err
	}
	status := StackStatus(string(descr.StackStatus))
	if status.InProgress() {
		return "", &ErrStackUpdateInProgress{
			Name: stack.Name,
		}
	}
	return c.update(stack)
}

// Delete removes an existing CloudFormation stack.
// If the stack doesn't exist then do nothing.
func (c *CloudFormation) Delete(stack *Stack) error {
	_, err := c.client.DeleteStack(c.ctx, &cloudformation.DeleteStackInput{
		StackName: aws.String(stack.Name),
	}, func(opts *cloudformation.Options) {
		opts.Region = stack.Region
	})
	if err != nil {
		if !stackDoesNotExist(err) {
			return fmt.Errorf("delete stack %s: %w", stack.Name, err)
		}
		// Move on if stack is already deleted.
	}
	return nil
}

// Describe returns a description of an existing stack.
// If the stack does not exist, returns ErrStackNotFound.
func (c *CloudFormation) Describe(stack *Stack) (*StackDescription, error) {
	out, err := c.client.DescribeStacks(c.ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(stack.Name),
	}, func(opts *cloudformation.Options) {
		opts.Region = stack.Region
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
	descr := StackDescription(out.Stacks[0])
	return &descr, nil
}

// Exists returns true if the CloudFormation stack exists, false otherwise.
// If an error occurs for another reason than ErrStackNotFound, then returns the error.
func (c *CloudFormation) Exists(stack *Stack) (bool, error) {
	if _, err := c.Describe(stack); err != nil {
		var notFound *ErrStackNotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Outputs returns the outputs of a stack description.
func (c *CloudFormation) Outputs(stack *Stack) (map[string]string, error) {
	stackDescription, err := c.Describe(stack)
	if err != nil {
		return nil, fmt.Errorf("retrieve outputs of stack description: %w", err)
	}
	outputs := make(map[string]string)
	for _, output := range stackDescription.Outputs {
		outputs[*output.OutputKey] = *output.OutputValue
	}
	return outputs, nil
}

func (c *CloudFormation) create(stack *Stack) (string, error) {
	cs, err := newCreateChangeSet(c.ctx, c.client, stack.Region, stack.Name, stack.Generation)
	if err != nil {
		return "", err
	}
	if err := cs.createAndExecute(stack.stackConfig); err != nil {
		return "", err
	}
	return cs.name, nil
}

func (c *CloudFormation) update(stack *Stack) (string, error) {
	cs, err := newUpdateChangeSet(c.ctx, c.client, stack.Region, stack.Name, stack.Generation)
	if err != nil {
		return "", err
	}
	if err := cs.createAndExecute(stack.stackConfig); err != nil {
		return "", err
	}
	return cs.name, nil
}
