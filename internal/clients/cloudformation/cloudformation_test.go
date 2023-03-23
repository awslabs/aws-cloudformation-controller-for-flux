// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	sdktypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation/mocks"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

const (
	mockStackName       = "mock-stack-1234"
	mockChangeSetName   = "flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc"
	mockChangeSetArn    = "arn:aws:cloudformation:us-west-2:111:changeSet/mock-31323334-3536-4738-b930-313233333435/9edc39b0-ee18-440d-823e-3dda74646b2"
	mockRegion          = "mock-region"
	mockGenerationId    = 2
	mockSourceRevision  = "main@sha1:132f4e719209eb10b9485302f8593fc0e680f4fc"
	mockBucket          = "mock-bucket"
	mockTemplateContent = "hello world"
	mockTemplateUrl     = "mock-url"
)

var (
	errDoesNotExist = &sdktypes.StackNotFoundException{
		Message:           aws.String("does not exist"),
		ErrorCodeOverride: aws.String("ValidationError"),
	}
	errChangeSetDoesNotExist = &sdktypes.ChangeSetNotFoundException{}
	genericApiError          = errors.New("problem")
)

func generateMockStack() *types.Stack {
	return &types.Stack{
		Name:           mockStackName,
		Region:         mockRegion,
		Generation:     mockGenerationId,
		SourceRevision: mockSourceRevision,
		StackConfig: &types.StackConfig{
			TemplateBucket: mockBucket,
			TemplateBody:   mockTemplateContent,
			TemplateURL:    mockTemplateUrl,
		},
	}
}

func TestCloudFormation_DescribeStack(t *testing.T) {
	testCases := map[string]struct {
		createMock  func(ctrl *gomock.Controller) client
		wantedDescr *types.StackDescription
		wantedErr   error
	}{
		"return error if describe call fails": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("describe stack mock-stack-1234: %w", genericApiError),
		},
		"return ErrStackNotFound if stack does not exist": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDoesNotExist)
				return m
			},
			wantedErr: &ErrStackNotFound{name: mockStackName},
		},
		"returns ErrStackNotFound if the list returned is empty": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Any(), gomock.Any()).Return(&cloudformation.DescribeStacksOutput{
					Stacks: []sdktypes.Stack{},
				}, nil)
				return m
			},
			wantedErr: &ErrStackNotFound{name: mockStackName},
		},
		"return ErrStackNotFound if stack has a change set but has not been created yet": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DescribeStacksInput{
					StackName: aws.String(mockStackName),
				}
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DescribeStacksOutput{
					Stacks: []sdktypes.Stack{
						{
							StackName:   aws.String(mockStackName),
							StackStatus: sdktypes.StackStatusReviewInProgress,
						},
					},
				}, nil)
				return m
			},
			wantedErr: &ErrStackNotFound{name: mockStackName},
		},
		"return ErrStackNotFound if stack is deleted": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DescribeStacksInput{
					StackName: aws.String(mockStackName),
				}
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DescribeStacksOutput{
					Stacks: []sdktypes.Stack{
						{
							StackName:   aws.String(mockStackName),
							StackStatus: sdktypes.StackStatusDeleteComplete,
						},
					},
				}, nil)
				return m
			},
			wantedErr: &ErrStackNotFound{name: mockStackName},
		},
		"returns a StackDescription if stack exists": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DescribeStacksInput{
					StackName: aws.String(mockStackName),
				}
				m.EXPECT().DescribeStacks(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DescribeStacksOutput{
					Stacks: []sdktypes.Stack{
						{
							StackName:   aws.String(mockStackName),
							StackStatus: sdktypes.StackStatusCreateComplete,
						},
					},
				}, nil)
				return m
			},
			wantedDescr: &types.StackDescription{
				StackName:   aws.String(mockStackName),
				StackStatus: sdktypes.StackStatusCreateComplete,
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			mockStack := generateMockStack()
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}

			// WHEN
			descr, err := c.DescribeStack(mockStack)

			// THEN
			require.Equal(t, tc.wantedDescr, descr)
			require.Equal(t, tc.wantedErr, err)
		})
	}
}

func TestCloudFormation_DescribeChangeSet(t *testing.T) {
	testCases := map[string]struct {
		createMock         func(ctrl *gomock.Controller, mockStack *types.Stack) client
		wantedDescr        *types.ChangeSetDescription
		wantedErr          error
		wantedChangeSetArn string
	}{
		"return error if describe call fails": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("describe change set flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc for stack mock-stack-1234: %w", genericApiError),
		},
		"return ErrChangeSetNotFound if stack does not exist": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDoesNotExist)
				return m
			},
			wantedErr: &ErrChangeSetNotFound{name: mockChangeSetName, stackName: mockStackName},
		},
		"return ErrChangeSetNotFound if change set does not exist": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangeSetDoesNotExist)
				return m
			},
			wantedErr: &ErrChangeSetNotFound{name: mockChangeSetName, stackName: mockStackName},
		},
		"returns ErrChangeSetEmpty if the change set returned is empty": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
					ChangeSetId:  aws.String(mockChangeSetArn),
					Status:       sdktypes.ChangeSetStatusCreateComplete,
					StatusReason: aws.String("NO_CHANGES_REASON"),
				}, nil)
				return m
			},
			wantedErr:          &ErrChangeSetEmpty{name: mockChangeSetName, stackName: mockStackName, Arn: mockChangeSetArn},
			wantedChangeSetArn: mockChangeSetArn,
		},
		"returns ErrChangeSetNotFound if the change set returned is deleted": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
					ChangeSetId:  aws.String(mockChangeSetArn),
					Status:       sdktypes.ChangeSetStatusDeleteComplete,
					StatusReason: aws.String("hello world"),
				}, nil)
				return m
			},
			wantedErr:          &ErrChangeSetNotFound{name: mockChangeSetName, stackName: mockStackName},
			wantedChangeSetArn: mockChangeSetArn,
		},
		"returns a ChangeSetDescription if change set exists": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DescribeChangeSetInput{
					StackName:     aws.String(mockStackName),
					ChangeSetName: aws.String(mockChangeSetName),
				}
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
					ChangeSetId:     aws.String(mockChangeSetArn),
					ChangeSetName:   aws.String(mockChangeSetName),
					Status:          sdktypes.ChangeSetStatusCreateComplete,
					ExecutionStatus: sdktypes.ExecutionStatusAvailable,
					StatusReason:    aws.String("hello"),
					Changes: []sdktypes.Change{
						{
							ResourceChange: &sdktypes.ResourceChange{
								ResourceType: aws.String("AWS::ECS::Service"),
							},
						},
					},
				}, nil)
				return m
			},
			wantedDescr: &types.ChangeSetDescription{
				Arn:             mockChangeSetArn,
				Status:          sdktypes.ChangeSetStatusCreateComplete,
				ExecutionStatus: sdktypes.ExecutionStatusAvailable,
				StatusReason:    "hello",
				Changes: []sdktypes.Change{
					{
						ResourceChange: &sdktypes.ResourceChange{
							ResourceType: aws.String("AWS::ECS::Service"),
						},
					},
				},
			},
			wantedChangeSetArn: mockChangeSetArn,
		},
		"returns a ChangeSetDescription when looking up change set by ARN": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				mockStack.ChangeSetArn = mockChangeSetArn

				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DescribeChangeSetInput{
					StackName:     aws.String(mockStackName),
					ChangeSetName: aws.String(mockChangeSetArn),
				}
				m.EXPECT().DescribeChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
					ChangeSetId:     aws.String(mockChangeSetArn),
					ChangeSetName:   aws.String(mockChangeSetName),
					Status:          sdktypes.ChangeSetStatusCreateComplete,
					ExecutionStatus: sdktypes.ExecutionStatusAvailable,
					StatusReason:    aws.String("hello"),
					Changes: []sdktypes.Change{
						{
							ResourceChange: &sdktypes.ResourceChange{
								ResourceType: aws.String("AWS::ECS::Service"),
							},
						},
					},
				}, nil)
				return m
			},
			wantedDescr: &types.ChangeSetDescription{
				Arn:             mockChangeSetArn,
				Status:          sdktypes.ChangeSetStatusCreateComplete,
				ExecutionStatus: sdktypes.ExecutionStatusAvailable,
				StatusReason:    "hello",
				Changes: []sdktypes.Change{
					{
						ResourceChange: &sdktypes.ResourceChange{
							ResourceType: aws.String("AWS::ECS::Service"),
						},
					},
				},
			},
			wantedChangeSetArn: mockChangeSetArn,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			mockStack := generateMockStack()
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl, mockStack),
				ctx:    ctx,
			}

			// WHEN
			descr, err := c.DescribeChangeSet(mockStack)

			// THEN
			require.Equal(t, tc.wantedDescr, descr)
			require.Equal(t, tc.wantedErr, err)
			require.Equal(t, tc.wantedChangeSetArn, mockStack.ChangeSetArn)
		})
	}

	t.Run("calls DescribeChangeSet repeatedly if there is a next token", func(t *testing.T) {
		// GIVEN
		mockStack := generateMockStack()
		ctrl, ctx := gomock.WithContext(context.Background(), t)
		defer ctrl.Finish()

		m := mocks.NewMockclient(ctrl)

		wantedChanges := []sdktypes.Change{
			{
				ResourceChange: &sdktypes.ResourceChange{
					ResourceType: aws.String("AWS::ECS::Service"),
				},
			},
			{
				ResourceChange: &sdktypes.ResourceChange{
					ResourceType: aws.String("AWS::ECS::Cluster"),
				},
			},
		}
		gomock.InOrder(
			m.EXPECT().DescribeChangeSet(gomock.Any(), &cloudformation.DescribeChangeSetInput{
				StackName:     aws.String(mockStackName),
				ChangeSetName: aws.String(mockChangeSetName),
				NextToken:     nil,
			}, gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
				ChangeSetId:     aws.String(mockChangeSetArn),
				ChangeSetName:   aws.String(mockChangeSetName),
				Status:          sdktypes.ChangeSetStatusCreateInProgress,
				ExecutionStatus: sdktypes.ExecutionStatusUnavailable,
				Changes: []sdktypes.Change{
					wantedChanges[0],
				},
				NextToken: aws.String("1111"),
			}, nil),
			m.EXPECT().DescribeChangeSet(gomock.Any(), &cloudformation.DescribeChangeSetInput{
				StackName:     aws.String(mockStackName),
				ChangeSetName: aws.String(mockChangeSetName),
				NextToken:     aws.String("1111"),
			}, gomock.Any()).Return(&cloudformation.DescribeChangeSetOutput{
				ChangeSetId:     aws.String(mockChangeSetArn),
				ChangeSetName:   aws.String(mockChangeSetName),
				Status:          sdktypes.ChangeSetStatusCreateInProgress,
				ExecutionStatus: sdktypes.ExecutionStatusUnavailable,
				Changes: []sdktypes.Change{
					wantedChanges[1],
				},
			}, nil),
		)

		cfn := CloudFormation{
			client: m,
			ctx:    ctx,
		}

		// WHEN
		out, err := cfn.DescribeChangeSet(mockStack)

		// THEN
		require.NoError(t, err)
		require.Equal(t, wantedChanges, out.Changes)
	})
}

func TestCloudFormation_CreateStack(t *testing.T) {
	testCases := map[string]struct {
		inStack    *types.Stack
		createMock func(ctrl *gomock.Controller) client
		wantedErr  error
	}{
		"return error if create call fails": {
			inStack: generateMockStack(),
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().CreateChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("create change set flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc for stack mock-stack-1234: %w", genericApiError),
		},
		"creates the stack": {
			inStack: generateMockStack(),
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.CreateChangeSetInput{
					ChangeSetName:       aws.String(mockChangeSetName),
					StackName:           aws.String(mockStackName),
					Description:         aws.String("Managed by Flux"),
					ChangeSetType:       sdktypes.ChangeSetTypeCreate,
					TemplateURL:         aws.String(mockTemplateUrl),
					IncludeNestedStacks: aws.Bool(true),
					Capabilities: []sdktypes.Capability{
						sdktypes.CapabilityCapabilityIam,
						sdktypes.CapabilityCapabilityNamedIam,
						sdktypes.CapabilityCapabilityAutoExpand,
					},
				}
				m.EXPECT().CreateChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.CreateChangeSetOutput{
					Id: aws.String(mockChangeSetArn),
				}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}

			// WHEN
			arn, err := c.CreateStack(tc.inStack)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, mockChangeSetArn, arn)
				require.Equal(t, mockChangeSetArn, tc.inStack.ChangeSetArn)
			}
		})
	}
}

func TestCloudFormation_UpdateStack(t *testing.T) {
	testCases := map[string]struct {
		inStack    *types.Stack
		createMock func(ctrl *gomock.Controller) client
		wantedErr  error
	}{
		"return error if update call fails": {
			inStack: generateMockStack(),
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().CreateChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("create change set flux-2-main-sha1-132f4e719209eb10b9485302f8593fc0e680f4fc for stack mock-stack-1234: %w", genericApiError),
		},
		"creates the stack": {
			inStack: generateMockStack(),
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.CreateChangeSetInput{
					ChangeSetName:       aws.String(mockChangeSetName),
					StackName:           aws.String(mockStackName),
					Description:         aws.String("Managed by Flux"),
					ChangeSetType:       sdktypes.ChangeSetTypeUpdate,
					TemplateURL:         aws.String(mockTemplateUrl),
					IncludeNestedStacks: aws.Bool(true),
					Capabilities: []sdktypes.Capability{
						sdktypes.CapabilityCapabilityIam,
						sdktypes.CapabilityCapabilityNamedIam,
						sdktypes.CapabilityCapabilityAutoExpand,
					},
				}
				m.EXPECT().CreateChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.CreateChangeSetOutput{
					Id: aws.String(mockChangeSetArn),
				}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}

			// WHEN
			arn, err := c.UpdateStack(tc.inStack)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, mockChangeSetArn, arn)
				require.Equal(t, mockChangeSetArn, tc.inStack.ChangeSetArn)
			}
		})
	}
}

func TestCloudFormation_ExecuteChangeSet(t *testing.T) {
	testCases := map[string]struct {
		createMock func(ctrl *gomock.Controller) client
		wantedErr  error
	}{
		"return error if execute call fails": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().ExecuteChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("execute change set arn:aws:cloudformation:us-west-2:111:changeSet/mock-31323334-3536-4738-b930-313233333435/9edc39b0-ee18-440d-823e-3dda74646b2 for stack mock-stack-1234: %w", genericApiError),
		},
		"executes the change set": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.ExecuteChangeSetInput{
					ChangeSetName: aws.String(mockChangeSetArn),
					StackName:     aws.String(mockStackName),
				}
				m.EXPECT().ExecuteChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.ExecuteChangeSetOutput{}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}
			mockStack := generateMockStack()
			mockStack.ChangeSetArn = mockChangeSetArn

			// WHEN
			err := c.ExecuteChangeSet(mockStack)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCloudFormation_ContinueStackRollback(t *testing.T) {
	testCases := map[string]struct {
		createMock func(ctrl *gomock.Controller) client
		wantedErr  error
	}{
		"return error if continue-rollback call fails": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().ContinueUpdateRollback(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: genericApiError,
		},
		"continues the stack rollback": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.ContinueUpdateRollbackInput{
					StackName: aws.String(mockStackName),
				}
				m.EXPECT().ContinueUpdateRollback(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.ContinueUpdateRollbackOutput{}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}
			mockStack := generateMockStack()

			// WHEN
			err := c.ContinueStackRollback(mockStack)

			// THEN
			if tc.wantedErr != nil {
				require.EqualError(t, err, tc.wantedErr.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCloudFormation_DeleteStack(t *testing.T) {
	testCases := map[string]struct {
		createMock func(ctrl *gomock.Controller) client
		wantedErr  error
	}{
		"return error if delete call fails": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DeleteStack(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("delete stack mock-stack-1234: %w", genericApiError),
		},
		"succeed if stack does not exist": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DeleteStack(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDoesNotExist)
				return m
			},
		},
		"deletes the stack": {
			createMock: func(ctrl *gomock.Controller) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DeleteStackInput{
					StackName: aws.String(mockStackName),
				}
				m.EXPECT().DeleteStack(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DeleteStackOutput{}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			mockStack := generateMockStack()
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl),
				ctx:    ctx,
			}

			// WHEN
			err := c.DeleteStack(mockStack)

			// THEN
			require.Equal(t, tc.wantedErr, err)
		})
	}
}

func TestCloudFormation_DeleteChangeSet(t *testing.T) {
	testCases := map[string]struct {
		createMock func(ctrl *gomock.Controller, mockStack *types.Stack) client
		wantedErr  error
	}{
		"return error if delete call fails": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DeleteChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, genericApiError)
				return m
			},
			wantedErr: fmt.Errorf("delete change set arn:aws:cloudformation:us-west-2:111:changeSet/mock-31323334-3536-4738-b930-313233333435/9edc39b0-ee18-440d-823e-3dda74646b2 for stack mock-stack-1234: %w", genericApiError),
		},
		"succeed if stack does not exist": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DeleteChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDoesNotExist)
				return m
			},
		},
		"succeed if change set does not exist": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				m.EXPECT().DeleteChangeSet(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangeSetDoesNotExist)
				return m
			},
		},
		"deletes the change set": {
			createMock: func(ctrl *gomock.Controller, mockStack *types.Stack) client {
				m := mocks.NewMockclient(ctrl)
				expectedIn := &cloudformation.DeleteChangeSetInput{
					StackName:     aws.String(mockStackName),
					ChangeSetName: aws.String(mockChangeSetArn),
				}
				m.EXPECT().DeleteChangeSet(gomock.Any(), gomock.Eq(expectedIn), gomock.Any()).Return(&cloudformation.DeleteChangeSetOutput{}, nil)
				return m
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			// GIVEN
			mockStack := generateMockStack()
			mockStack.ChangeSetArn = mockChangeSetArn
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()
			c := CloudFormation{
				client: tc.createMock(ctrl, mockStack),
				ctx:    ctx,
			}

			// WHEN
			err := c.DeleteChangeSet(mockStack)

			// THEN
			require.Equal(t, tc.wantedErr, err)
		})
	}
}
