# AWS CloudFormation Template Sync Controller for Flux installation guide

This document covers the installation of the AWS CloudFormation Template Sync Controller for Flux into your Kubernetes cluster.

For instructions for running the CloudFormation controller on a local Kubernetes cluster,
see the [development guide](./developing.md#run-the-cloudformation-controller-on-a-local-kind-cluster).

<!-- toc -->

1. [Prerequisites](#prerequisites)
1. [AWS credentials](#aws-credentials)
1. [TODO](#todo)
1. [Security recommendations](#security-recommendations)
   1. [Kubernetes cluster security][#kubernetes-cluster-security]
   1. [IAM permissions](#iam-permissions)
<!-- tocstop -->

## Prerequisites

These instructions assume you already have a Kubernetes cluster with Flux installed. For instructions on installing Flux into your Kubernetes cluster, see the [Flux documentation](https://fluxcd.io/flux/get-started/).

These instructions also assume that you already created the following prerequisite resources.
You can use the [sample CloudFormation template](../examples/resources.yaml) for creating these resources.

* **Flux configuration Git repository**:
These instructions for installing the CloudFormation controller assume that Flux is configured to
manage itself from a Git repository, for example using the `flux bootstrap` command.
* At least one **CloudFormation template repository**:
Through the Flux source controller, the CloudFormation controller can deploy CloudFormation templates
stored in [a Git repository](https://fluxcd.io/flux/components/source/gitrepositories/) such as an AWS CodeCommit repository,
in [a bucket](https://fluxcd.io/flux/components/source/buckets/) such as an Amazon S3 bucket,
or in [an OCI repository](https://fluxcd.io/flux/components/source/ocirepositories/) such as an Amazon ECR repository.
See [the Flux documentation](https://fluxcd.io/flux/guides/repository-structure/)
for a guide to various approaches for organizing the repositories that store your CloudFormation templates.
* **Amazon S3 bucket**:
The CloudFormation controller requires an S3 bucket.
When the controller syncs a CloudFormation template into a CloudFormation stack in your AWS account,
it will first upload the template to S3 and then provide a link to the S3 object to CloudFormation.
To minimize storage costs, you can safely enable a lifecycle rule on the S3 bucket to expire
objects after one day.

## AWS credentials

The CloudFormation controller relies on the [default behavior of the AWS SDK for Go V2](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/#specifying-credentials) to determine the AWS credentials that it uses to authenticate with AWS APIs.

The CloudFormation controller searches for credentials in the following order:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
1. Web identity token credentials (including running in an Amazon EKS cluster using IAM roles for service accounts)
1. Shared credentials and config ini files (`~/.aws/credentials`, `~/.aws/config`)
1. Amazon Elastic Container Service (Amazon ECS) task metadata service
1. Amazon Elastic Compute Cloud (Amazon EC2) instance metadata service

As a best practice, we recommend that you use short lived AWS credentials for the CloudFormation controller, for example using [IAM roles for service accounts on an EKS cluster](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).



## TODO instructions

TODO

## Security recommendations

### Kubernetes cluster security

We recommend following all [security best practices defined by the Flux project](https://fluxcd.io/flux/security/best-practices/)
when configuring your Flux components and the CloudFormation controller.  We also recommend following the
[Flux project's additional security best practices for shared cluster multi-tenancy](https://fluxcd.io/flux/security/best-practices/#additional-best-practices-for-shared-cluster-multi-tenancy),
including node isolation and network isolation for your Flux components and the CloudFormation controller.

For information on how to achieve node isolation and network isolation for your Flux components and the CloudFormation
controller on Amazon EKS clusters, see the following EKS Best Practices Guides:will
* [Isolating tenant workloads to specific nodes](https://aws.github.io/aws-eks-best-practices/security/docs/multitenancy/#isolating-tenant-workloads-to-specific-nodes)
* [Network security using Kubernetes network policies](https://aws.github.io/aws-eks-best-practices/security/docs/network/#network-policy)
* [Network security using AWS VPC Security Groups](https://aws.github.io/aws-eks-best-practices/security/docs/network/#security-groups)

### IAM permissions

We recommend that the AWS credentials used by the CloudFormation controller have the least-privileged permissions needed
to deploy your CloudFormation stacks in your AWS account.

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
may need the following permissions.

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