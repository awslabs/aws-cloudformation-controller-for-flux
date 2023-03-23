// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package s3

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/awslabs/aws-cloudformation-controller-for-flux/internal/clients/s3/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

const (
	awsAccountId  = "123456789012"
	templateBody  = "hello"
	mockBucket    = "mockBucket"
	mockRegion    = "mockRegion"
	mockObjectKey = "mockFileName"
)

func TestS3_Upload(t *testing.T) {
	testCases := map[string]struct {
		mockS3ManagerClient func(m *mocks.Mocks3ManagerAPI)
		mockStsClient       func(m *mocks.MockstsAPI)

		wantedURL string
		wantError error
	}{
		"return error if STS call fails": {
			mockStsClient: func(m *mocks.MockstsAPI) {
				m.EXPECT().GetCallerIdentity(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(nil, errors.New("some STS error"))
			},
			mockS3ManagerClient: func(m *mocks.Mocks3ManagerAPI) {
				m.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			},
			wantError: fmt.Errorf("some STS error"),
		},
		"return error if upload fails": {
			mockS3ManagerClient: func(m *mocks.Mocks3ManagerAPI) {
				expectedIn := &s3.PutObjectInput{
					Body:                strings.NewReader(templateBody),
					Bucket:              aws.String(mockBucket),
					Key:                 aws.String(mockObjectKey),
					ACL:                 types.ObjectCannedACLBucketOwnerFullControl,
					ExpectedBucketOwner: aws.String(awsAccountId),
				}
				m.EXPECT().Upload(
					gomock.Any(),
					gomock.Eq(expectedIn),
					gomock.Any(),
				).Return(nil, errors.New("some upload error"))
			},
			mockStsClient: func(m *mocks.MockstsAPI) {
				m.EXPECT().GetCallerIdentity(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(&sts.GetCallerIdentityOutput{
					Account: aws.String(awsAccountId),
				}, nil)
			},
			wantError: fmt.Errorf("some upload error"),
		},
		"should upload to the s3 bucket": {
			mockS3ManagerClient: func(m *mocks.Mocks3ManagerAPI) {
				expectedIn := &s3.PutObjectInput{
					Body:                strings.NewReader(templateBody),
					Bucket:              aws.String(mockBucket),
					Key:                 aws.String(mockObjectKey),
					ACL:                 types.ObjectCannedACLBucketOwnerFullControl,
					ExpectedBucketOwner: aws.String(awsAccountId),
				}
				m.EXPECT().Upload(
					gomock.Any(),
					gomock.Eq(expectedIn),
					gomock.Any(),
				).Return(&manager.UploadOutput{
					Location: "mockURL",
				}, nil)
			},
			mockStsClient: func(m *mocks.MockstsAPI) {
				m.EXPECT().GetCallerIdentity(
					gomock.Any(),
					gomock.Any(),
					gomock.Any(),
				).Return(&sts.GetCallerIdentityOutput{
					Account: aws.String(awsAccountId),
				}, nil)
			},
			wantedURL: "mockURL",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctrl, ctx := gomock.WithContext(context.Background(), t)
			defer ctrl.Finish()

			mockS3ManagerClient := mocks.NewMocks3ManagerAPI(ctrl)
			tc.mockS3ManagerClient(mockS3ManagerClient)

			mockStsClient := mocks.NewMockstsAPI(ctrl)
			tc.mockStsClient(mockStsClient)

			service := S3{
				manager:   mockS3ManagerClient,
				stsClient: mockStsClient,
				ctx:       ctx,
			}

			gotURL, gotErr := service.UploadTemplate(mockBucket, mockRegion, mockObjectKey, strings.NewReader(templateBody))

			if gotErr != nil {
				require.EqualError(t, gotErr, tc.wantError.Error())
			} else {
				require.Equal(t, gotErr, nil)
				require.Equal(t, gotURL, tc.wantedURL)
			}
		})
	}
}
