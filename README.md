# AWS CloudFormation Template Sync Controller for Flux

TODO:
* Demo gif
* Package as helm chart
* Support for stack parameters
* Support for stack tags
* Support for declaring dependency on other CFN stacks
* Support for managing stacks cross-account (see https://aws-controllers-k8s.github.io/community/docs/user-docs/cross-account-resource-management/)
* Support for managing stacks cross-region (see https://aws-controllers-k8s.github.io/community/docs/user-docs/multi-region-resource-management/)
* Support for per-namespace S3 buckets

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
| Run against local cluster | `make run` |
| See CloudFormation stacks | `kubectl describe cfnstack -A` |
| Clean up | `make clean` |

## Set up Flux on a local kind cluster

1. Create an S3 bucket to hold the temporary template files that get uploaded to CloudFormation:

```
$ ACCOUNT_ID=`aws sts get-caller-identity --query 'Account' --output text`

$ aws s3api create-bucket \
    --region us-west-2 \
    --bucket flux-templates-$ACCOUNT_ID \
    --create-bucket-configuration LocationConstraint=us-west-2

$ aws s3api put-bucket-lifecycle-configuration \
    --region us-west-2 \
    --bucket flux-templates-$ACCOUNT_ID \
    --lifecycle-configuration '{ "Rules": [ { "ID": "one_day", "Prefix": "", "Status": "Enabled", "Expiration": { "Days": 1 } } ] }'
```

2. Set up HTTPS access to CodeCommit for an IAM user using Git credentials:

https://docs.aws.amazon.com/codecommit/latest/userguide/setting-up-gc.html

3. Create a CodeCommit repository to store a sample CloudFormation template, then clone it:
```
$ aws codecommit create-repository --region us-west-2 --repository-name my-cloudformation-templates

$ git clone https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-cloudformation-templates

$ cd my-cloudformation-templates

$ git checkout --orphan main
```

4. Create a file `template.yaml` in the sample template repo with the following contents:
```yaml
Resources:
  SampleResource:
    Type: AWS::SSM::Parameter
    Properties:
      Type: String
      Value: "Hello World"
```

5. Push the sample template file to the repo:
```
$ git add template.yaml

$ git commit -m "Sample template"

$ git push --set-upstream origin main

$ cd ../
```

6. Create another CodeCommit repository to store the Flux configuration for your local cluster:
```
$ aws codecommit create-repository --region us-west-2 --repository-name flux-local-kind-cluster
```

7. Create a file `kind-cluster.yaml` with the following contents to expose the source controller API endpoint to your host on port 30000:
```yaml
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30000
    hostPort: 30000
```

8. Create your kind cluster using that configuration:
```
$ kind create cluster --config=kind-cluster.yaml
```

9. Bootstrap Flux on the local cluster, which will push some initial configuration to your Flux config repository:
```
$ flux check --pre

$ export CODECOMMIT_USERNAME=<CodeCommit username>
$ export CODECOMMIT_PASSWORD=<CodeCommit password>

$ flux bootstrap git \
    --url=https://git-codecommit.us-west-2.amazonaws.com/v1/repos/flux-local-kind-cluster \
    --branch=main \
    --token-auth=true \
    --username=$CODECOMMIT_USERNAME \
    --password=$CODECOMMIT_PASSWORD

$ flux create secret git cfn-template-repo-auth \
    --url=https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-cloudformation-templates \
    --username=$CODECOMMIT_USERNAME \
    --password=$CODECOMMIT_PASSWORD
```

10. Install the CloudFormation Flux CRDs into the local cluster:
```
make install
```

11. Clone your Flux config repository:
```
$ git clone https://git-codecommit.us-west-2.amazonaws.com/v1/repos/flux-local-kind-cluster

$ cd flux-local-kind-cluster
```

12. Open the file `flux-system/gotk-components.yaml` in your Flux config repository and find the Service object named source-controller.  Update the object's configuration like this:
```diff
 apiVersion: v1
 kind: Service
 metadata:
   labels:
     app.kubernetes.io/component: source-controller
     app.kubernetes.io/instance: flux-system
     app.kubernetes.io/part-of: flux
     app.kubernetes.io/version: v0.38.3
     control-plane: controller
   name: source-controller
   namespace: flux-system
 spec:
   ports:
   - name: http
     port: 80
     protocol: TCP
     targetPort: http
+    nodePort: 30000
   selector:
     app: source-controller
-  type: ClusterIP
+  type: NodePort
```

13. Create a file `cfn-sample-repo.yaml` in your Flux config repository with the following contents:
```yaml
apiVersion: source.toolkit.fluxcd.io/v1beta1
kind: GitRepository
metadata:
  name: cfn-sample-template-repo
  namespace: flux-system
spec:
  interval: 1h
  ref:
    branch: main
  url: https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-cloudformation-templates
  secretRef:
    name: cfn-template-repo-auth
```

14. Create a file `cfn-sample-stack.yaml` in your Flux config repository with the following contents:
```yaml
apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
kind: CloudFormationStack
metadata:
  name: cfn-sample-stack
  namespace: flux-system
spec:
  stackName: flux-cfn-controller-sample-stack
  region: us-west-2
  templatePath: ./template.yaml
  sourceRef:
    kind: GitRepository
    name: cfn-sample-template-repo
  interval: 1h
  retryInterval: 5m
```

15. Push the files into the repo:
```
$ git add flux-system/gotk-components.yaml

$ git commit -m "Expose source controller on node port 30000"

$ git add cfn-sample-repo.yaml cfn-sample-stack.yaml

$ git commit -m "Add sample CFN stack"

$ git push
```

16. Ensure that the sample template repo is successfully hooked up to Flux:
```
$ flux get sources git
```

17. Create an ECR repository:
```
$ aws ecr create-repository --repository-name aws-cloudformation-controller-for-flux --region us-west-2
```

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This library is licensed under the MIT-0 License. See the LICENSE file.
