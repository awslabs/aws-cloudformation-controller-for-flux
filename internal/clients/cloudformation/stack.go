// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"bytes"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

var (
	successfulDeploymentStackStatuses = []types.StackStatus{
		types.StackStatusCreateComplete,
		types.StackStatusUpdateComplete,
		types.StackStatusImportComplete,
	}

	inProgressStackStatuses = []types.StackStatus{
		types.StackStatusCreateInProgress,
		types.StackStatusDeleteInProgress,
		types.StackStatusRollbackInProgress,
		types.StackStatusUpdateCompleteCleanupInProgress,
		types.StackStatusUpdateInProgress,
		types.StackStatusUpdateRollbackCompleteCleanupInProgress,
		types.StackStatusUpdateRollbackInProgress,
		types.StackStatusImportInProgress,
		types.StackStatusImportRollbackInProgress,
	}

	unrecoverableFailureStackStatuses = []types.StackStatus{
		types.StackStatusCreateFailed,
		types.StackStatusDeleteFailed,
		types.StackStatusRollbackComplete,
		types.StackStatusRollbackFailed,
	}

	recoverableFailureStackStatuses = []types.StackStatus{
		types.StackStatusUpdateFailed,
		types.StackStatusUpdateRollbackComplete,
		types.StackStatusImportRollbackComplete,
		types.StackStatusImportRollbackFailed,
	}
)

// Stack represents a AWS CloudFormation stack.
type Stack struct {
	Name         string
	Region       string
	Generation   int64
	ChangeSetArn string
	*stackConfig
}

type stackConfig struct {
	TemplateBody string
	TemplateURL  string
	Parameters   []types.Parameter
	Tags         []types.Tag
}

// StackOption allows you to initialize a Stack with additional properties.
type StackOption func(s *Stack)

// NewStack creates a stack with the given name and template body.
func NewStack(name string, template *bytes.Buffer, opts ...StackOption) *Stack {
	s := &Stack{
		Name: name,
		stackConfig: &stackConfig{
			TemplateBody: template.String(),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewStackWithURL creates a stack with a URL to the template.
func NewStackWithURL(name string, templateURL string, opts ...StackOption) *Stack {
	s := &Stack{
		Name: name,
		stackConfig: &stackConfig{
			TemplateURL: templateURL,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithParameters passes parameters to a stack.
func WithParameters(params map[string]string) StackOption {
	return func(s *Stack) {
		var flatParams []types.Parameter
		for k, v := range params {
			flatParams = append(flatParams, types.Parameter{
				ParameterKey:   aws.String(k),
				ParameterValue: aws.String(v),
			})
		}
		s.Parameters = flatParams
	}
}

// WithTags applies the tags to a stack.
func WithTags(tags map[string]string) StackOption {
	return func(s *Stack) {
		var flatTags []types.Tag
		for k, v := range tags {
			flatTags = append(flatTags, types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
		s.Tags = flatTags
	}
}

// StackEvent is an alias the SDK's StackEvent type.
type StackEvent types.StackEvent

// StackDescription is an alias the SDK's Stack type.
type StackDescription types.Stack

// StackResource is an alias the SDK's StackResource type.
type StackResource types.StackResource

// SDK returns the underlying struct from the AWS SDK.
func (d *StackDescription) SDK() *types.Stack {
	raw := types.Stack(*d)
	return &raw
}

// RequiresCleanup returns true if the stack was created or deleted, but the action failed and the stack should be deleted.
func (d *StackDescription) RequiresCleanup() bool {
	return stackStatusListContains(d.StackStatus, unrecoverableFailureStackStatuses)
}

// ReadyForStackCleanup returns true if the stack is in a state where it can be deleted.
func (d *StackDescription) ReadyForCleanup() bool {
	return !d.InProgress()
}

// RequiresRollbackContinuation returns true if the stack failed an update, and the rollback failed.
// The only valid actions for the stack in this state are the ContinueUpdateRollback or DeleteStack operations
func (d *StackDescription) RequiresRollbackContinuation() bool {
	return types.StackStatusUpdateRollbackFailed == d.StackStatus
}

// InProgress returns true if the stack is currently being deployed.
func (d *StackDescription) InProgress() bool {
	return stackStatusListContains(d.StackStatus, inProgressStackStatuses)
}

// IsSuccess returns true if the stack mutated successfully.
func (d *StackDescription) IsSuccess() bool {
	return stackStatusListContains(d.StackStatus, successfulDeploymentStackStatuses)
}

// IsRecoverableFailure returns true if the stack failed to mutate, but can be further updated.
func (d *StackDescription) IsRecoverableFailure() bool {
	return stackStatusListContains(d.StackStatus, recoverableFailureStackStatuses)
}

func stackStatusListContains(element types.StackStatus, statusList []types.StackStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}
