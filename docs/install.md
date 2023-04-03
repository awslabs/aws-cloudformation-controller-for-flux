# AWS CloudFormation Template Sync Controller for Flux installation guide

This document covers the installation of the AWS CloudFormation Template Sync Controller for Flux into your Kubernetes cluster.

You can find instructions for running the CloudFormation controller on a local Kubernetes cluster in the
[development guide](./developing.md#run-the-cloudformation-controller-on-a-local-kind-cluster).

<!-- toc -->

1. [Prerequisites](#prerequisites)
1. [Create AWS credentials](#create-aws-credentials)
1. [Register the CloudFormation controller repository with Flux](#register-the-cloudformation-controller-repository-with-flux)
1. [Deploy the CloudFormation controller](#deploy-the-cloudformation-controller)
   1. [Use IAM roles for service accounts on an Amazon EKS cluster](#option-1-short-lived-credentials-using-iam-roles-for-service-accounts-on-an-eks-cluster-recommended) (recommended)
   2. [Use AWS credentials in environment variables](#option-2-long-term-credentials-as-environment-variables)
   3. [Use AWS credentials in a mounted file](#option-3-long-term-credentials-in-a-mounted-credentials-file)
1. [Security recommendations](#security-recommendations)
   1. [Kubernetes cluster security](#kubernetes-cluster-security)
   1. [Kubernetes user permissions](#kubernetes-user-permissions)
   1. [AWS IAM permissions](#aws-iam-permissions)
<!-- tocstop -->

## Prerequisites

These instructions assume you already have a Kubernetes cluster with Flux installed.
See the [Flux documentation](https://fluxcd.io/flux/get-started/) for instructions
on installing Flux into your Kubernetes cluster.

These instructions also assume that you already created the following prerequisite resources.
You can use the [sample CloudFormation template](../examples/resources.yaml) for creating these resources.

* **Flux configuration repository**:
These instructions assume that Flux is configured to manage itself from a Git repository,
for example using the `flux bootstrap` command.
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

## Create AWS credentials

The CloudFormation controller relies on the [default behavior of the AWS SDK for Go V2](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/#specifying-credentials) to determine the AWS credentials that it uses to authenticate with AWS APIs.

The CloudFormation controller searches for credentials in the following order:

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
1. Web identity token credentials (including running in an Amazon EKS cluster using IAM roles for service accounts)
1. Shared credentials and config ini files (`~/.aws/credentials`, `~/.aws/config`)
1. Amazon Elastic Container Service (Amazon ECS) task metadata service
1. Amazon Elastic Compute Cloud (Amazon EC2) instance metadata service

As a best practice, we recommend that you use short lived AWS credentials for the CloudFormation controller, for example using [IAM roles for service accounts on an EKS cluster](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

We also recommend that the AWS credentials used by the CloudFormation controller have the least privileged permissions needed
to deploy your CloudFormation stacks in your AWS account.  See the [AWS IAM permissions](#aws-iam-permissions) section below
for example policies to attach to your IAM user or role.

For example, the following commands use eksctl and the AWS CLI to create an IAM OIDC identity provider for your EKS cluster,
create an IAM policy and IAM role, and associate the IAM role with a Kubernetes service account for the CloudFormation controller
in your EKS cluster's flux-system namespace.

```bash
$ eksctl utils associate-iam-oidc-provider --cluster my-cluster --approve

$ aws iam create-policy --policy-name my-policy --policy-document file://my-policy.json

$ eksctl create iamserviceaccount \
    --cluster my-cluster \
    --namespace flux-system \
    --name cfn-controller \
    --role-only \
    --role-name "AWSCloudFormationControllerFluxIRSARole" \
    --attach-policy-arn arn:aws:iam::123456789012:policy/my-policy \
    --approve
```

## Register the CloudFormation controller repository with Flux

In your Flux configuration repository, create a file named `cfn-controller-source.yaml` with the configuration below
to register this GitHub repository with your Flux installation.

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: GitRepository
metadata:
  name: aws-cloudformation-controller-for-flux
  namespace: flux-system
spec:
  interval: 1h
  timeout: 60s
  ref:
    branch: main
  url: https://github.com/awslabs/aws-cloudformation-controller-for-flux
```

Update the GitRepository `.spec.ref` field to point to the CloudFormation controller release version or commit ID you want to deploy.
For example, replace `branch: main` with `tag: v0.0.1` to pin the installation to CloudFormation controller version 0.0.1.

Commit and push the `cfn-controller-source.yaml` file to your Flux configuration repository.

Run the following commands to verify that Flux can successfully connect to this repository.

```bash
$ flux reconcile source git flux-system
$ flux reconcile source git aws-cloudformation-controller-for-flux
```

## Deploy the CloudFormation controller

In your Flux configuration repository, create a file named `cfn-controller.yaml`, fill in the appropriate contents,
then commit and push the file to your Flux configuration repository.
The contents of the `cfn-controller.yaml` file depends on how you choose to provide AWS credentials
to the CloudFormation controller.
In all cases, you will need to know the name of the AWS region where the CloudFormation controller
should deploy CloudFormation stacks and the name of the S3 bucket where the CloudFormation controller
should upload CloudFormation templates.

### Option 1: Short-lived credentials using IAM roles for service accounts on an EKS cluster (recommended)

Copy and paste the following configuration into the `cfn-controller.yaml` file.
Update the `eks.amazonaws.com/role-arn` value with the correct IAM role ARN.
Update the `AWS_REGION` and `TEMPLATE_BUCKET` values with the correct values for your region and S3 bucket.

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: aws-cloudformation-controller-for-flux
  namespace: flux-system
spec:
  interval: 5m
  path: ./config/default
  prune: true
  wait: true
  timeout: 5m
  sourceRef:
    kind: GitRepository
    name: aws-cloudformation-controller-for-flux
  patches:
    - patch: |
        apiVersion: v1
        kind: ServiceAccount
        metadata:
          name: cfn-controller
          annotations:
            eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/AWSCloudFormationControllerFluxIRSARole
      target:
        kind: ServiceAccount
        name: cfn-controller
    - patch: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: cfn-controller
        spec:
          template:
            spec:
              containers:
              - name: manager
                env:
                  - name: AWS_REGION
                    value: "us-west-2"
                  - name: TEMPLATE_BUCKET
                    value: "my-cloudformation-templates-bucket"
      target:
        kind: Deployment
        name: cfn-controller
```

### Option 2: Long-term credentials as environment variables

Create a Kubernetes secret that contains your IAM user's credentials.  First, encode your credentials:

```bash
$ echo -n 'FAKEAWSACCESSKEYID' | base64 -w 0
RkFLRUFXU0FDQ0VTU0tFWUlE

$ echo -n 'FAKEAWSSECRETACCESSKEY' | base64 -w 0
RkFLRUFXU1NFQ1JFVEFDQ0VTU0tFWQ==
```

Then create a file named `secret.yaml` containing the Kubernetes secret configuration:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-creds-for-cfn-controller
  namespace: flux-system
type: Opaque
data:
  AWS_ACCESS_KEY_ID: RkFLRUFXU0FDQ0VTU0tFWUlE
  AWS_SECRET_ACCESS_KEY: RkFLRUFXU1NFQ1JFVEFDQ0VTU0tFWQ==
```

Apply the secret to your Kubernetes cluster, then delete the configuration file.

```bash
$ kubectl apply -f secret.yaml
$ rm secret.yaml
```

Copy and paste the following configuration into the `cfn-controller.yaml` file.
Update the `AWS_REGION` and `TEMPLATE_BUCKET` values with the correct values for your region and S3 bucket.

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: aws-cloudformation-controller-for-flux
  namespace: flux-system
spec:
  interval: 5m
  path: ./config/default
  prune: true
  wait: true
  timeout: 5m
  sourceRef:
    kind: GitRepository
    name: aws-cloudformation-controller-for-flux
  patches:
    - patch: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: cfn-controller
        spec:
          template:
            spec:
              containers:
              - name: manager
                env:
                  - name: AWS_REGION
                    value: "us-west-2"
                  - name: TEMPLATE_BUCKET
                    value: "my-cloudformation-templates-bucket"
                  - name: AWS_ACCESS_KEY_ID
                    valueFrom:
                      secretKeyRef:
                        name: aws-creds-for-cfn-controller
                        key: AWS_ACCESS_KEY_ID
                  - name: AWS_SECRET_ACCESS_KEY
                    valueFrom:
                      secretKeyRef:
                        name: aws-creds-for-cfn-controller
                        key: AWS_SECRET_ACCESS_KEY
      target:
        kind: Deployment
        name: cfn-controller
```

### Option 3: Long-term credentials in a mounted credentials file

Create a file named `credentials` that contains your IAM user's credentials:

```ini
[default]
aws_access_key_id=FAKEAWSACCESSKEYID
aws_secret_access_key=FAKEAWSSECRETACCESSKEY
```

Create a Kubernetes secret that contains credentials file, then delete the credentials file.
```bash
$ kubectl create secret generic aws-creds-for-cfn-controller -n flux-system --from-file ./credentials
$ rm ./credentials
```

Copy and paste the following configuration into the `cfn-controller.yaml` file.
Update the `AWS_REGION` and `TEMPLATE_BUCKET` values with the correct values for your region and S3 bucket.

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: aws-cloudformation-controller-for-flux
  namespace: flux-system
spec:
  interval: 5m
  path: ./config/default
  prune: true
  wait: true
  timeout: 5m
  sourceRef:
    kind: GitRepository
    name: aws-cloudformation-controller-for-flux
  patches:
    - patch: |
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: cfn-controller
        spec:
          template:
            spec:
              containers:
              - name: manager
                env:
                  - name: AWS_REGION
                    value: "us-west-2"
                  - name: TEMPLATE_BUCKET
                    value: "my-cloudformation-templates-bucket"
                volumeMounts:
                - name: aws-creds
                  mountPath: "/.aws"
                  readOnly: true
              volumes:
              - name: aws-creds
                secret:
                  secretName: aws-creds-for-cfn-controller
      target:
        kind: Deployment
        name: cfn-controller
```

## Security recommendations

### Kubernetes cluster security

We recommend following all [security best practices defined by the Flux project](https://fluxcd.io/flux/security/best-practices/)
when configuring your Flux components and the CloudFormation controller.  We also recommend following the
[Flux project's additional security best practices for shared cluster multi-tenancy](https://fluxcd.io/flux/security/best-practices/#additional-best-practices-for-shared-cluster-multi-tenancy),
including node isolation and network isolation for your Flux components and the CloudFormation controller.

For information on how to achieve node isolation and network isolation for your Flux components and the CloudFormation
controller on Amazon EKS clusters, see the following EKS Best Practices Guides:
* [Isolating tenant workloads to specific nodes](https://aws.github.io/aws-eks-best-practices/security/docs/multitenancy/#isolating-tenant-workloads-to-specific-nodes)
* [Network security using Kubernetes network policies](https://aws.github.io/aws-eks-best-practices/security/docs/network/#network-policy)
* [Network security using AWS VPC Security Groups](https://aws.github.io/aws-eks-best-practices/security/docs/network/#security-groups)

### Kubernetes user permissions

We recommend that users with access to your Kubernetes cluster have the least privileged permissions needed for interacting
with the CloudFormation controller.  We have provided two sample Kubernetes roles that can be used to grant permissions to your users.
* [Sample CloudFormationStack editor role](../config/rbac/cfnstack_editor_role.yaml)
* [Sample CloudFormationStack viewer role](../config/rbac/cfnstack_viewer_role.yaml)

### AWS IAM permissions

We recommend that the AWS credentials used by the CloudFormation controller have the least privileged permissions needed
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