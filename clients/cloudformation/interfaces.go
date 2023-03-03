// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
)

type changeSetAPI interface {
	CreateChangeSet(ctx context.Context, params *cloudformation.CreateChangeSetInput, optFns ...func(*cloudformation.Options)) (*cloudformation.CreateChangeSetOutput, error)
	DescribeChangeSet(ctx context.Context, params *cloudformation.DescribeChangeSetInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeChangeSetOutput, error)
	ExecuteChangeSet(ctx context.Context, params *cloudformation.ExecuteChangeSetInput, optFns ...func(*cloudformation.Options)) (*cloudformation.ExecuteChangeSetOutput, error)
	DeleteChangeSet(ctx context.Context, params *cloudformation.DeleteChangeSetInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DeleteChangeSetOutput, error)
}

type client interface {
	changeSetAPI

	GetTemplateSummary(ctx context.Context, params *cloudformation.GetTemplateSummaryInput, optFns ...func(*cloudformation.Options)) (*cloudformation.GetTemplateSummaryOutput, error)
	DescribeStacks(ctx context.Context, params *cloudformation.DescribeStacksInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error)
	DescribeStackEvents(ctx context.Context, params *cloudformation.DescribeStackEventsInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeStackEventsOutput, error)
	DescribeStackResources(ctx context.Context, params *cloudformation.DescribeStackResourcesInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DescribeStackResourcesOutput, error)
	GetTemplate(ctx context.Context, params *cloudformation.GetTemplateInput, optFns ...func(*cloudformation.Options)) (*cloudformation.GetTemplateOutput, error)
	DeleteStack(ctx context.Context, params *cloudformation.DeleteStackInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DeleteStackOutput, error)
}
