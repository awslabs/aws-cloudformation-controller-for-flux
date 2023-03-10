// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

// Package s3 provides a client to make API requests to Amazon S3.
package s3

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
)

const (
	// Error codes.
	errCodeNotFound = "NotFound"
)

// S3 wraps an Amazon S3 client.
type S3 struct {
	manager   s3ManagerAPI
	client    s3API
	stsClient stsAPI
	ctx       context.Context
}

// New returns an S3 client.
func New(ctx context.Context) (*S3, error) {
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithAPIOptions([]func(*middleware.Stack) error{
			awsmiddleware.AddUserAgentKey("cfn-flux-controller"),
		}),
	)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg)
	stsClient := sts.NewFromConfig(cfg)

	return &S3{
		client:    client,
		manager:   manager.NewUploader(client),
		stsClient: stsClient,
		ctx:       ctx,
	}, nil
}

// Upload uploads a template file to an S3 bucket under the specified key.
// Returns an object URL that can be passed directly to CloudFormation
func (s *S3) UploadTemplate(bucket, region, key string, data io.Reader) (string, error) {
	url, err := s.upload(bucket, region, key, data)
	if err != nil {
		return "", err
	}
	return url, nil
}

func (s *S3) upload(bucket, region, key string, buf io.Reader) (string, error) {
	// The expected bucket owner is the current caller
	identityResp, err := s.stsClient.GetCallerIdentity(s.ctx, &sts.GetCallerIdentityInput{}, func(opts *sts.Options) {
		if region != "" {
			opts.Region = region
		}
	})
	if err != nil {
		return "", err
	}
	expectedBucketOwner := identityResp.Account

	in := &s3.PutObjectInput{
		Body:   buf,
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		// Per s3's recommendation, the bucket owner, in addition to the
		// object owner, is granted full control.
		// https://docs.aws.amazon.com/AmazonS3/latest/userguide/about-object-ownership.html
		ACL:                 types.ObjectCannedACLBucketOwnerFullControl,
		ExpectedBucketOwner: expectedBucketOwner,
	}
	var opts []func(*manager.Uploader)
	if region != "" {
		opts = append(opts, manager.WithUploaderRequestOptions(
			func(opts *s3.Options) {
				if region != "" {
					opts.Region = region
				}
			},
		))
	}
	resp, err := s.manager.Upload(s.ctx, in, opts...)
	if err != nil {
		return "", err
	}
	return resp.Location, nil
}
