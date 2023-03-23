// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package clients

import (
	"io"

	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
)

type CloudFormationClient interface {
	// Stack methods
	CreateStack(stack *types.Stack) (changeSetArn string, err error)
	UpdateStack(stack *types.Stack) (changeSetArn string, err error)
	DescribeStack(stack *types.Stack) (*types.StackDescription, error)
	DeleteStack(stack *types.Stack) error
	ContinueStackRollback(stack *types.Stack) error

	// Change set methods
	ExecuteChangeSet(stack *types.Stack) error
	DeleteChangeSet(stack *types.Stack) error
	DescribeChangeSet(stack *types.Stack) (*types.ChangeSetDescription, error)
}

type S3Client interface {
	UploadTemplate(bucket, region, key string, data io.Reader) (string, error)
}
