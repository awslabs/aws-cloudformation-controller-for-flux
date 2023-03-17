// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aws/smithy-go"
)

// ErrChangeSetEmpty occurs when the change set does not contain any new or updated resources.
type ErrChangeSetEmpty struct {
	name      string
	stackName string
	Arn       string
}

func (e *ErrChangeSetEmpty) Error() string {
	return fmt.Sprintf("change set with name %s for stack %s has no changes", e.name, e.stackName)
}

// ErrStackNotFound occurs when a CloudFormation stack does not exist.
type ErrStackNotFound struct {
	name string
}

func (e *ErrStackNotFound) Error() string {
	return fmt.Sprintf("stack named %s cannot be found", e.name)
}

// ErrChangeSetNotFound occurs when a CloudFormation changeset does not exist.
type ErrChangeSetNotFound struct {
	name      string
	stackName string
}

func (e *ErrChangeSetNotFound) Error() string {
	return fmt.Sprintf("change set with name %s for stack %s cannot be found", e.name, e.stackName)
}

// stackDoesNotExist returns true if the underlying error is a stack doesn't exist.
func stackDoesNotExist(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "ValidationError":
			// A ValidationError occurs if we describe a stack which doesn't exist.
			if strings.Contains(ae.ErrorMessage(), "does not exist") {
				return true
			}
		}
	}
	return false
}

// changeSetDoesNotExist returns true if the underlying error is a change set doesn't exist.
func changeSetDoesNotExist(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "ChangeSetNotFound":
			return true
		}
	}
	return false
}
