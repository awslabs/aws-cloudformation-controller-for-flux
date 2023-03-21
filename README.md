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

## Example

Connect a git repository to Flux - this git repository will store your CloudFormation templates:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta1
kind: GitRepository
metadata:
  name: my-gitops-repo
  namespace: flux-system
spec:
  url: https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-gitops-repo
  ref:
    branch: main
  interval: 5m
  secretRef:
    name: my-gitops-repo-auth
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
    name: my-gitops-repo
  interval: 10m
  retryInterval: 5m
```

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

## Development

For information about developing the CloudFormation controller locally, see [Developing the AWS CloudFormation Template Sync Controller for Flux](./docs/developing.md).

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This library is licensed under the MIT-0 License. See the LICENSE file.
