// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package types

import (
	sdktypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

var (
	successfulDeploymentStackStatuses = []sdktypes.StackStatus{
		sdktypes.StackStatusCreateComplete,
		sdktypes.StackStatusUpdateComplete,
		sdktypes.StackStatusImportComplete,
	}

	inProgressStackStatuses = []sdktypes.StackStatus{
		sdktypes.StackStatusCreateInProgress,
		sdktypes.StackStatusDeleteInProgress,
		sdktypes.StackStatusRollbackInProgress,
		sdktypes.StackStatusUpdateCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateInProgress,
		sdktypes.StackStatusUpdateRollbackCompleteCleanupInProgress,
		sdktypes.StackStatusUpdateRollbackInProgress,
		sdktypes.StackStatusImportInProgress,
		sdktypes.StackStatusImportRollbackInProgress,
	}

	unrecoverableFailureStackStatuses = []sdktypes.StackStatus{
		sdktypes.StackStatusCreateFailed,
		sdktypes.StackStatusDeleteFailed,
		sdktypes.StackStatusRollbackComplete,
		sdktypes.StackStatusRollbackFailed,
	}

	recoverableFailureStackStatuses = []sdktypes.StackStatus{
		sdktypes.StackStatusUpdateFailed,
		sdktypes.StackStatusUpdateRollbackComplete,
		sdktypes.StackStatusImportRollbackComplete,
		sdktypes.StackStatusImportRollbackFailed,
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
	Parameters     []sdktypes.Parameter
	Tags           []sdktypes.Tag
}

// StackEvent is an alias the SDK's StackEvent type.
type StackEvent sdktypes.StackEvent

// StackDescription is an alias the SDK's Stack type.
type StackDescription sdktypes.Stack

// StackResource is an alias the SDK's StackResource type.
type StackResource sdktypes.StackResource

// SDK returns the underlying struct from the AWS SDK.
func (d *StackDescription) SDK() *sdktypes.Stack {
	raw := sdktypes.Stack(*d)
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
	return sdktypes.StackStatusUpdateRollbackFailed == d.StackStatus
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

// DeleteFailed returns true if the stack is in DELETE_FAILED state
func (d *StackDescription) DeleteFailed() bool {
	return sdktypes.StackStatusDeleteFailed == d.StackStatus
}

func stackStatusListContains(element sdktypes.StackStatus, statusList []sdktypes.StackStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}
