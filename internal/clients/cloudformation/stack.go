// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
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
	Name           string
	Region         string
	Generation     int64
	SourceRevision string
	ChangeSetArn   string
	*StackConfig
}

type StackConfig struct {
	TemplateBucket string
	TemplateBody   string
	TemplateURL    string
	Parameters     []types.Parameter
	Tags           []types.Tag
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
