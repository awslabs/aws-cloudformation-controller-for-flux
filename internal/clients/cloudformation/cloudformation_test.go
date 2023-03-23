// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package cloudformation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/cloudformation/mocks"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

const (
	mockChangeSetName = "copilot-31323334-3536-4738-b930-313233333435"
	mockChangeSetID   = "arn:aws:cloudformation:us-west-2:111:changeSet/copilot-31323334-3536-4738-b930-313233333435/9edc39b0-ee18-440d-823e-3dda74646b2"
)

func TestCloudFormation_DescribeChangeSet(t *testing.T) {
	t.Run("returns an error if the DescribeChangeSet action fails", func(t *testing.T) {
		// GIVEN
		ctrl, ctx := gomock.WithContext(context.Background(), t)
		defer ctrl.Finish()

		m := mocks.NewMockclient(ctrl)
		m.EXPECT().DescribeChangeSet(ctx, gomock.Any(), gomock.Any()).Return(nil, errors.New("some error"))
		cfn := CloudFormation{
			client: m,
			ctx:    ctx,
		}

		// WHEN
		stack := &types.Stack{
			Name:         "phonetool-test",
			ChangeSetArn: mockChangeSetID,
		}
		out, err := cfn.DescribeChangeSet(stack)

		// THEN
		require.EqualError(t, err, fmt.Sprintf("describe change set %s for stack phonetool-test: some error", mockChangeSetID))
		require.Nil(t, out)
	})
}
