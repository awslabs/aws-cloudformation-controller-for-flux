# AWS CloudFormation Template Sync Controller for Flux

The AWS CloudFormation Template Sync Controller for Flux helps you to store CloudFormation templates in a git repository
and have Flux automatically sync template changes to CloudFormation stacks in your AWS account.

[Flux](https://fluxcd.io/) is an open source GitOps framework that can be installed into your Kubernetes cluster.
Using Flux, you can store infrastructure as code files in a git repository and have them automatically deployed.
Flux monitors git repositories (as well as S3 buckets and OCI repositories like Amazon ECR) for changes to configuration
files like Kubernetes manifests and Helm charts and applies them automatically to your Kubernetes cluster.

The AWS CloudFormation Template Sync Controller for Flux is an extension to Flux that lets you store your CloudFormation
templates in a git repository (as well as S3 buckets and OCI repositories like Amazon ECR) and have them automatically
deployed by Flux to CloudFormation stacks in your AWS account.  After installing the CloudFormation controller into your
Kubernetes cluster, you can configure Flux to monitor your git repository for changes to CloudFormation template files.
When a CloudFormation template file is updated in a git commit, the CloudFormation controller will deploy the latest
template to your CloudFormation stack.  The CloudFormation controller continuously ensures that the latest template from
the git repository is synced into your stack by re-deploying the template on a regular interval.

## Demo

![Demo](/docs/demo.gif 'Demo')

## Development

For information about developing the CloudFormation controller locally, see [Developing the AWS CloudFormation Template Sync Controller for Flux](./docs/developing.md).

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This library is licensed under the MIT-0 License. See the LICENSE file.
