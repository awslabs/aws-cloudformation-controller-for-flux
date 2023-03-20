#!/bin/bash

# This script creates a new local kind cluster, and installs Flux and the CFN controller onto it.
# The cluster is created with temporary ECR credentials that expire after 12 hours.

set -e

export AWS_REGION=us-west-2
export AWS_ACCOUNT_ID=`aws sts get-caller-identity --query 'Account' --output text`

# Create required AWS resources

echo Deploying CloudFormation stack and setting git credentials

aws cloudformation deploy --stack-name flux-cfn-controller-resources --region $AWS_REGION --template-file examples/resources.yaml --capabilities CAPABILITY_NAMED_IAM

existing_creds=`aws iam list-service-specific-credentials --user-name flux-git --service-name codecommit.amazonaws.com --query 'ServiceSpecificCredentials'`
empty_creds="[]"

if [ "$existing_creds" = "$empty_creds" ]; then
    new_creds=`aws iam create-service-specific-credential --user-name flux-git --service-name codecommit.amazonaws.com --query 'ServiceSpecificCredential' --output json`
    aws secretsmanager put-secret-value --region $AWS_REGION --secret-string "$new_creds" --secret-id flux-git-credentials
fi

creds=`aws secretsmanager get-secret-value --region us-west-2 --secret-id flux-git-credentials --query 'SecretString' --output text`

export CODECOMMIT_USERNAME=`echo $creds | jq -r '.ServiceUserName'`
export CODECOMMIT_PASSWORD=`echo $creds | jq -r '.ServicePassword'`

# Set up the kind cluster

echo Creating the kind cluster

kind delete cluster

kind create cluster --config=local-dev/kind-cluster-config.yaml

# Install Flux

echo Installing flux into the kind cluster

flux check --pre

flux bootstrap git \
    --url=https://git-codecommit.$AWS_REGION.amazonaws.com/v1/repos/my-flux-configuration \
    --branch=main \
    --token-auth=true \
    --username=$CODECOMMIT_USERNAME \
    --password=$CODECOMMIT_PASSWORD \

flux create secret git cfn-template-repo-auth \
    --url=https://git-codecommit.$AWS_REGION.amazonaws.com/v1/repos/my-cloudformation-templates \
    --username=$CODECOMMIT_USERNAME \
    --password=$CODECOMMIT_PASSWORD

rm -rf patch-local-cluster
mkdir patch-local-cluster
cd patch-local-cluster
git clone https://git-codecommit.us-west-2.amazonaws.com/v1/repos/my-flux-configuration
cd my-flux-configuration
git apply ../../local-dev/local-flux-dev-config.patch
git add flux-system
git commit -m "Expose source controller locally"
git push
cd ../..
rm -rf patch-local-cluster
flux reconcile source git flux-system

# Install CFN controller types

echo Installing CloudFormation controller resource types into the kind cluster

make install

flux reconcile kustomization flux-system

flux reconcile source git flux-system

kubectl get all --namespace flux-system

flux get all

# Install secrets into the cluster

echo Installing credentials into the kind cluster

# Note that this ECR token will expire, so this is only for development use
kubectl delete secret docker-registry ecr-cred -n flux-system --ignore-not-found
kubectl create secret docker-registry ecr-cred --docker-server=$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com --docker-username=AWS --docker-password=`aws ecr get-login-password --region $AWS_REGION` -n flux-system
kubectl patch serviceaccount default -p "{\"imagePullSecrets\": [{\"name\": \"ecr-cred\"}]}" -n flux-system

kubectl delete secret aws-creds -n flux-system --ignore-not-found
kubectl create secret generic aws-creds -n flux-system --from-file ~/.aws/credentials
