# Developing the AWS CloudFormation Template Sync Controller for Flux

Learn how to develop the AWS CloudFormation Template Sync Controller for Flux.

## Set up a local development environment

### Install required tools

1. Install go 1.19+

2. Run `make install-tools`

3. Install kind and create a kind cluster:

https://kind.sigs.k8s.io/docs/user/quick-start

4. Install the Flux CLI:
```
$ curl -s https://fluxcd.io/install.sh | sudo bash
```

### Useful commands

|  | Command |
| ------ | ----------- |
| Generate CRDs | `make generate` |
| Build | `make build` |
| Test | `make test` |
| Integration Test | `make integ-test` |
| See CloudFormation stacks | `kubectl describe cfnstack -A` |
| View logs | `kubectl logs -l app=cfn-controller --namespace flux-system` |
| Clean up | `make clean` |

## Run the CloudFormation controller on a local kind cluster

1. Bootstrap a local kind cluster that runs Flux:
```
$ make bootstrap-local-cluster
```

2. Clone your git repository created by the previous step:
```
$ git clone https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-cloudformation-templates
$ cd my-cloudformation-templates
```

3. Copy the example CloudFormation templates found in examples/my-cloudformation-templates/ into your CFN template git repository. Then, push the sample template files to the repo:
```
$ git add -A
$ git commit -m "Sample template"
$ git push --set-upstream origin main
```

4. Clone your Flux configuration repository created by the bootstrap step:
```
$ git clone https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-flux-configuration
$ cd my-flux-configuration
```

5. Copy the example Flux configuration file `examples/my-flux-configuration/my-cloudformation-templates-repo.yaml` into your Flux config git repository. Then, push the config file to the repo:
```
$ git add my-cloudformation-templates-repo.yaml
$ git commit -m "Register CFN templates repo with Flux"
$ git push --set-upstream origin main
$ flux reconcile source git flux-system
$ flux get sources git
```

6. Copy one of the example CloudFormationStack configuration files `examples/my-flux-configuration/my-cloudformation-stack.yaml` into your Flux config git repository:
```
$ git add my-cloudformation-stack.yaml
$ git commit -m "Register a CFN stack with Flux"
$ git push
$ flux reconcile source git flux-system
$ kubectl describe cfnstack -A
```

7. Build the CloudFormation controller into a Docker image, then deploy it to your local cluster:
```
$ make deploy-local
```

## Run the CloudFormation controller outside of your local kind cluster

For development purposes, it can be slow to build your controller source code into a Docker image
and deploy the new image to your cluster.  Instead, you can run the controller outside
of a container and have it connect to your local cluster.

Follow the previous section's steps to set up Flux on a local kind cluster.
Instead of running `make deploy-local` at the end, run:
```
make run
```
