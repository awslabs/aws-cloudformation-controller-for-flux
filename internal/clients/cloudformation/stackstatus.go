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

// StackStatus represents the status of a stack.
type StackStatus string

// RequiresCleanup returns true if the stack was created or deleted, but the action failed and the stack should be deleted.
func (ss StackStatus) RequiresCleanup() bool {
	return stackStatusListContains(ss, unrecoverableFailureStackStatuses)
}

// ReadyForStackCleanup returns true if the stack is in a state where it can be deleted.
func (ss StackStatus) ReadyForStackCleanup() bool {
	return !ss.InProgress()
}

// RequiresRollbackContinuation returns true if the stack failed an update, and the rollback failed.
// The only valid actions for the stack in this state are the ContinueUpdateRollback or DeleteStack operations
func (ss StackStatus) RequiresRollbackContinuation() bool {
	return string(types.StackStatusUpdateRollbackFailed) == string(ss)
}

// InProgress returns true if the stack is currently being deployed.
func (ss StackStatus) InProgress() bool {
	return stackStatusListContains(ss, inProgressStackStatuses)
}

// IsSuccess returns true if the stack mutated successfully.
func (ss StackStatus) IsSuccess() bool {
	return stackStatusListContains(ss, successfulDeploymentStackStatuses)
}

// IsRecoverableFailure returns true if the stack failed to mutate, but can be further updated.
func (ss StackStatus) IsRecoverableFailure() bool {
	return stackStatusListContains(ss, recoverableFailureStackStatuses)
}

func stackStatusListContains(element StackStatus, statusList []types.StackStatus) bool {
	for _, status := range statusList {
		if string(element) == string(status) {
			return true
		}
	}
	return false
}

// String implements the fmt.Stringer interface.
func (ss StackStatus) String() string {
	return string(ss)
}
