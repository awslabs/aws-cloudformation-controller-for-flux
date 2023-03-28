#!/bin/bash

# This script creates a new local kind cluster, and installs Flux and the CFN controller onto it.
# The cluster is created with temporary ECR credentials that expire after 12 hours.

set -e

export AWS_REGION=us-west-2
export AWS_ACCOUNT_ID=`aws sts get-caller-identity --query 'Account' --output text`

# Create required AWS resources

if [[ ! -v CI ]]; then
    echo Deploying CloudFormation stack with prerequisite resources

    aws cloudformation deploy --stack-name flux-cfn-controller-resources --region $AWS_REGION --template-file examples/resources.yaml --capabilities CAPABILITY_NAMED_IAM

    existing_creds=`aws iam list-service-specific-credentials --user-name flux-git --service-name codecommit.amazonaws.com --query 'ServiceSpecificCredentials'`
    empty_creds="[]"

    if [ "$existing_creds" = "$empty_creds" ]; then
        new_creds=`aws iam create-service-specific-credential --user-name flux-git --service-name codecommit.amazonaws.com --query 'ServiceSpecificCredential' --output json`
        aws secretsmanager put-secret-value --region $AWS_REGION --secret-string "$new_creds" --secret-id flux-git-credentials
    fi
fi

echo Setting up git repository for CloudFormation templates

creds=`aws secretsmanager get-secret-value --region $AWS_REGION --secret-id flux-git-credentials --query 'SecretString' --output text`

export CODECOMMIT_USERNAME=`echo $creds | jq -r '.ServiceUserName'`
export CODECOMMIT_PASSWORD=`echo $creds | jq -r '.ServicePassword'`

default_branch=`aws codecommit get-repository --repository-name my-cloudformation-templates --query 'repositoryMetadata.defaultBranch' --output text`
no_default_branch="None"

if [ "$default_branch" = "$no_default_branch" ]; then
    rm -rf init-cfn-template-repo
    mkdir init-cfn-template-repo
    cd init-cfn-template-repo
    git clone https://$CODECOMMIT_USERNAME:$CODECOMMIT_PASSWORD@git-codecommit.$AWS_REGION.amazonaws.com/v1/repos/my-cloudformation-templates
    cd my-cloudformation-templates
    git checkout --orphan main
    echo My CloudFormation templates > README.md
    git add README.md
    git commit -m "Initial commit"
    git push --set-upstream origin main
    cd ../..
    rm -rf init-cfn-template-repo
fi

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

if [[ ! -v CI ]]; then
    rm -rf patch-local-cluster
    mkdir patch-local-cluster
    cd patch-local-cluster
    git clone https://$CODECOMMIT_USERNAME:$CODECOMMIT_PASSWORD@git-codecommit.$AWS_REGION.amazonaws.com/v1/repos/my-flux-configuration
    cd my-flux-configuration
    git apply ../../local-dev/local-flux-dev-config.patch
    git add flux-system
    git commit -m "Expose source controller locally"
    git push
    cd ../..
    rm -rf patch-local-cluster
    flux reconcile source git flux-system
fi

# Install CFN controller types

echo Installing CloudFormation controller resource types into the kind cluster

make install

flux reconcile kustomization flux-system

flux reconcile source git flux-system

kubectl get all --namespace flux-system

flux get all

# Install secrets into the cluster

echo Installing credentials into the kind cluster

kubectl delete secret aws-creds -n flux-system --ignore-not-found
if [[ ! -v CI ]]; then
    kubectl create secret generic aws-creds -n flux-system --from-file ~/.aws/credentials
fi
