// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package types

import (
	"strings"

	sdktypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

// ChangeSetDescription is the output of the DescribeChangeSet action.
type ChangeSetDescription struct {
	Arn             string
	Status          sdktypes.ChangeSetStatus
	ExecutionStatus sdktypes.ExecutionStatus
	StatusReason    string
	Changes         []sdktypes.Change
}

const (
	// Status reasons that can occur if the change set execution status is "FAILED".
	noChangesReason = "NO_CHANGES_REASON"
	noUpdatesReason = "NO_UPDATES_REASON"
)

var (
	inProgressChangeSetStatuses = []sdktypes.ChangeSetStatus{
		sdktypes.ChangeSetStatusCreateInProgress,
		sdktypes.ChangeSetStatusCreatePending,
		sdktypes.ChangeSetStatusDeleteInProgress,
		sdktypes.ChangeSetStatusDeletePending,
	}

	failedChangeSetStatuses = []sdktypes.ChangeSetStatus{
		sdktypes.ChangeSetStatusDeleteFailed,
		sdktypes.ChangeSetStatusFailed,
	}

	inProgressChangeSetExecutionStatuses = []sdktypes.ExecutionStatus{
		sdktypes.ExecutionStatusExecuteInProgress,
		sdktypes.ExecutionStatusUnavailable,
	}

	failedChangeSetExecutionStatuses = []sdktypes.ExecutionStatus{
		sdktypes.ExecutionStatusExecuteFailed,
		sdktypes.ExecutionStatusObsolete,
	}
)

func (d *ChangeSetDescription) IsEmpty() bool {
	return (len(d.Changes) == 0 && strings.Contains(d.StatusReason, "didn't contain changes")) ||
		d.StatusReason == noChangesReason ||
		d.StatusReason == noUpdatesReason
}

func (d *ChangeSetDescription) IsDeleted() bool {
	return d.Status == sdktypes.ChangeSetStatusDeleteComplete
}

func (d *ChangeSetDescription) IsCreated() bool {
	return d.Status == sdktypes.ChangeSetStatusCreateComplete
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
	return d.IsCreated() && d.ExecutionStatus == sdktypes.ExecutionStatusExecuteComplete
}

func (d *ChangeSetDescription) ReadyForExecution() bool {
	return d.IsCreated() && d.ExecutionStatus == sdktypes.ExecutionStatusAvailable
}

func changesetStatusListContains(element sdktypes.ChangeSetStatus, statusList []sdktypes.ChangeSetStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}

func changesetExecutionStatusListContains(element sdktypes.ExecutionStatus, statusList []sdktypes.ExecutionStatus) bool {
	for _, status := range statusList {
		if element == status {
			return true
		}
	}
	return false
}
