# AWS CloudFormation Template Sync Controller for Flux installation guide

This document covers the installation of the AWS CloudFormation Template Sync Controller for Flux into your Kubernetes cluster.

For instructions for running the CloudFormation controller on a local Kubernetes cluster,
see the [development guide](./developing.md#run-the-cloudformation-controller-on-a-local-kind-cluster).

<!-- toc -->

1. [Prerequisites](#prerequisites)
1. [AWS credentials](#aws-credentials)
1. [IAM permissions](#iam-permissions)
1. [TODO](#todo)
<!-- tocstop -->

## Prerequisites

These instructions assume you already have a Kubernetes cluster with Flux installed. For instructions on installing Flux into your Kubernetes cluster, see the [Flux documentation](https://fluxcd.io/flux/get-started/).

TODO: S3 bucket

## AWS credentials

The CloudFormation controller relies on the [default behavior of the AWS SDK for Go V2](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/#specifying-credentials) to determine the AWS credentials that it uses to authenticate with AWS APIs.

The CloudFormation controller searches for credentials in the following order:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
1. Web identity token credentials (including running in an Amazon EKS cluster using IAM roles for service accounts)
1. Shared credentials and config ini files (`~/.aws/credentials`, `~/.aws/config`)
1. Amazon Elastic Container Service (Amazon ECS) task metadata service
1. Amazon Elastic Compute Cloud (Amazon EC2) instance metadata service

As a best practice, we recommend that you use short lived AWS credentials for the CloudFormation controller, for example using [IAM roles for service accounts on an EKS cluster](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

## IAM permissions

The CloudFormation controller requires the following IAM permissions for managing CloudFormation stacks in your AWS account:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "cloudformation:ContinueUpdateRollback",
        "cloudformation:CreateChangeSet",
        "cloudformation:DeleteChangeSet",
        "cloudformation:DeleteStack",
        "cloudformation:DescribeChangeSet",
        "cloudformation:DescribeStacks",
        "cloudformation:ExecuteChangeSet"
      ],
      "Resource": [
        "arn:aws:cloudformation:us-west-2:123456789012:stack/*"
      ]
    }
  ]
}
```

The CloudFormation controller also requires the following IAM permissions to upload your CloudFormation templates to your S3 bucket:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:AbortMultipartUpload"
      ],
      "Resource": [
        "arn:aws:s3:::<your S3 bucket name>/flux-*.template"
      ]
    }
  ]
}
```

The CloudFormation controller also requires permissions on behalf of CloudFormation to download your CloudFormation
templates from your S3 bucket and to provision the resources defined in your CloudFormation templates.

For example, if your CloudFormation templates define `AWS::DynamoDB::Table` resources, the CloudFormation controller
will need the following permissions.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": [
        "arn:aws:s3:::<your S3 bucket name>/flux-*.template"
      ],
      "Condition": {
        "ForAnyValue:StringEquals": {
          "aws:CalledVia": [
            "cloudformation.amazonaws.com"
          ]
        }
      }
    },
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:CreateTable",
        "dynamodb:DeleteTable",
        "dynamodb:DescribeTable",
        "dynamodb:DescribeTimeToLive",
        "dynamodb:UpdateTimeToLive",
        "dynamodb:UpdateContributorInsights",
        "dynamodb:UpdateContinuousBackups",
        "dynamodb:DescribeContinuousBackups",
        "dynamodb:DescribeContributorInsights",
        "dynamodb:EnableKinesisStreamingDestination",
        "dynamodb:DisableKinesisStreamingDestination",
        "dynamodb:DescribeKinesisStreamingDestination",
        "dynamodb:ListTagsOfResource",
        "dynamodb:TagResource",
        "dynamodb:UntagResource",
        "dynamodb:UpdateTable",
      ],
      "Resource": "arn:aws:dynamodb:us-west-2:123456789012:table/*",
      "Condition": {
        "ForAnyValue:StringEquals": {
          "aws:CalledVia": [
            "cloudformation.amazonaws.com"
          ]
        }
      }
    }
  ]
}
```
