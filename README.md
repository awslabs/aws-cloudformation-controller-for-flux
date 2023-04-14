# AWS CloudFormation Template Sync Controller for Flux

[![Unit tests](https://github.com/awslabs/aws-cloudformation-controller-for-flux/actions/workflows/unit-tests.yml/badge.svg?branch=main)](https://github.com/awslabs/aws-cloudformation-controller-for-flux/actions/workflows/unit-tests.yml)
[![Integration tests](https://github.com/awslabs/aws-cloudformation-controller-for-flux/actions/workflows/integ-tests.yml/badge.svg?branch=main)](https://github.com/awslabs/aws-cloudformation-controller-for-flux/actions/workflows/integ-tests.yml)

The AWS CloudFormation Template Sync Controller for Flux helps you to store CloudFormation templates in a git repository
and automatically sync template changes to CloudFormation stacks in your AWS account with Flux.

[Flux CD](https://fluxcd.io/) is an open source, Cloud Native Computing Foundation (CNCF) graduated project that keeps
Kubernetes clusters in sync with sources of configuration including Git repositories, S3 buckets, and Open Container
Initiative (OCI) compatible repositories (such as Amazon ECR).

The AWS CloudFormation Template Sync Controller for Flux is an extension to Flux that lets you store your CloudFormation
templates in a Git repository and automatically deploy them as CloudFormation stacks in your AWS account. After installing
the CloudFormation Template Sync controller into your Kubernetes cluster, you can configure Flux to monitor your Git repository
for changes to CloudFormation template files. When a CloudFormation template file is updated in a Git commit, the CloudFormation
controller is designed to automatically deploy the latest template changes to your CloudFormation stack. The CloudFormation
controller is also designed to continuously sync the latest template from the Git repository into your stack by re-deploying
the template on a regular interval.

## Demo

![Demo](/docs/demo.gif 'Demo')

## Example

Connect a git repository to Flux - this git repository will store your CloudFormation templates:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: GitRepository
metadata:
  name: my-cfn-templates-repo
  namespace: flux-system
spec:
  url: https://github.com/clareliguori/my-cfn-templates
  ref:
    branch: main
  interval: 30s
  secretRef:
    name: my-cfn-templates-repo-auth
```

In your git repository, add a CloudFormation template file for each stack that you want automatically deployed by Flux:

```
hello-world/stack-template.yaml
hey-there/another-stack-template.yaml
hi-friend/my-stack-template.yaml
README.md
```

Register each CloudFormation template file in your git repository with Flux as a separate CloudFormation stack object:

```yaml
apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
kind: CloudFormationStack
metadata:
  name: hello-world-stack
  namespace: flux-system
spec:
  stackName: flux-hello-world
  templatePath: ./hello-world/stack-template.yaml
  sourceRef:
    kind: GitRepository
    name: my-cfn-templates-repo
  interval: 1h
  retryInterval: 5m
```

See the [CloudFormationStack API reference](./docs/api/cloudformationstack.md) for the full set of configuration options
supported by the CloudFormation controller for CloudFormation stacks.

When either the stack template file in the git repo OR the stack object in Flux is created or updated, you will see the CloudFormation stack created/updated in your AWS account:

```yaml
$ kubectl describe cfnstack hello-world-stack --namespace flux-system
Name:         cfn-sample-stack
Namespace:    flux-system
...
Status:
  Conditions:
    Last Transition Time:  2023-02-28T19:56:58Z
    Message:               deployed stack 'flux-hello-world'
    Observed Generation:   1
    Reason:                Succeeded
    Status:                True
    Type:                  Ready
```

## Installation

See the [AWS CloudFormation Template Sync Controller for Flux installation guide](./docs/install.md).

## API reference

See the [CloudFormationStack API reference](./docs/api/cloudformationstack.md) for the full set of configuration options
supported by the CloudFormation controller for CloudFormation stacks.

## Development

For information about developing the CloudFormation controller locally, see [Developing the AWS CloudFormation Template Sync Controller for Flux](./docs/developing.md).

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information on reporting security issues.

## License

This library is licensed under the MIT-0 License. See the LICENSE file.
