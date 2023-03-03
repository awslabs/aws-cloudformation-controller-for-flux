// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

var (
	successStackStatuses = []types.StackStatus{
		types.StackStatusCreateComplete,
		types.StackStatusDeleteComplete,
		types.StackStatusUpdateComplete,
		types.StackStatusUpdateCompleteCleanupInProgress,
		types.StackStatusImportComplete,
	}

	failureStackStatuses = []types.StackStatus{
		types.StackStatusCreateFailed,
		types.StackStatusDeleteFailed,
		types.StackStatusUpdateFailed,
		types.StackStatusRollbackInProgress,
		types.StackStatusRollbackComplete,
		types.StackStatusRollbackFailed,
		types.StackStatusUpdateRollbackComplete,
		types.StackStatusUpdateRollbackCompleteCleanupInProgress,
		types.StackStatusUpdateRollbackInProgress,
		types.StackStatusUpdateRollbackFailed,
		types.StackStatusImportRollbackInProgress,
		types.StackStatusImportRollbackComplete,
		types.StackStatusImportRollbackFailed,
	}

	inProgressStackStatuses = []types.StackStatus{
		types.StackStatusCreateInProgress,
		types.StackStatusRollbackInProgress,
		types.StackStatusDeleteInProgress,
		types.StackStatusUpdateInProgress,
		types.StackStatusUpdateCompleteCleanupInProgress,
		types.StackStatusUpdateRollbackInProgress,
		types.StackStatusUpdateRollbackCompleteCleanupInProgress,
		types.StackStatusReviewInProgress,
		types.StackStatusImportInProgress,
		types.StackStatusImportRollbackInProgress,
	}
)

// StackStatus represents the status of a stack.
type StackStatus string

// requiresCleanup returns true if the stack was created, but failed and should be deleted.
func (ss StackStatus) requiresCleanup() bool {
	return string(types.StackStatusRollbackComplete) == string(ss) || string(types.StackStatusRollbackFailed) == string(ss)
}

// InProgress returns true if the stack is currently being updated.
func (ss StackStatus) InProgress() bool {
	for _, inProgress := range inProgressStackStatuses {
		if string(ss) == string(inProgress) {
			return true
		}
	}
	return false
}

// UpsertInProgress returns true if the resource is updating or being created.
func (ss StackStatus) UpsertInProgress() bool {
	return string(types.StackStatusCreateInProgress) == string(ss) || string(types.StackStatusUpdateInProgress) == string(ss)
}

// IsSuccess returns true if the resource mutated successfully.
func (ss StackStatus) IsSuccess() bool {
	for _, success := range successStackStatuses {
		if string(ss) == string(success) {
			return true
		}
	}
	return false
}

// IsFailure returns true if the resource failed to mutate.
func (ss StackStatus) IsFailure() bool {
	for _, failure := range failureStackStatuses {
		if string(ss) == string(failure) {
			return true
		}
	}
	return false
}

// String implements the fmt.Stringer interface.
func (ss StackStatus) String() string {
	return string(ss)
}
