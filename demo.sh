#!/bin/bash

# Reset for demo:
# * Tear down the kind cluster
#      kind delete cluster
# * Delete the CloudFormation stacks from my AWS account
#      aws cloudformation delete-stack --stack-name my-cfn-stack-deployed-by-flux
#      aws cloudformation delete-stack --stack-name my-other-cfn-stack-deployed-by-flux
#      aws cloudformation delete-stack --stack-name yet-another-cfn-stack-deployed-by-flux
#      aws cloudformation wait stack-delete-complete --stack-name my-cfn-stack-deployed-by-flux
#      aws cloudformation wait stack-delete-complete --stack-name my-other-cfn-stack-deployed-by-flux
#      aws cloudformation wait stack-delete-complete --stack-name yet-another-cfn-stack-deployed-by-flux
# * Delete the stack files from testdata/my-flux-configuration (git push)
# * Re-copy the examples into testdata
#      cp -rf examples/my-cloudformation-templates/* testdata/my-cloudformation-templates/ (git push)
#      cp -rf examples/my-flux-configuration/* testdata/my-flux-configuration/
# * Re-create the kind cluster
#      make bootstrap-local-cluster
# * Start up local controller:
#      make run

export PATH="$PATH:$PWD/bin/local"
PS1="$ "

# Ensure demo-magic is cloned here
# https://github.com/paxtonhare/demo-magic
. ../demo-magic/demo-magic.sh

clear

# Look and feel

TYPE_SPEED=20
DEMO_COMMENT_COLOR=$CYAN
NO_WAIT=false

# Start the demo

# Press enter to continue
PROMPT_TIMEOUT=0
p "# Welcome to the AWS CloudFormation Template Sync Controller for Flux!"
PROMPT_TIMEOUT=1

NO_WAIT=true
p "#"
p "# Flux is a GitOps tool that runs on Kubernetes. Out of the box, Flux automates syncing"
p "# Kubernetes configuration from source locations like git repositories into your Kubernetes"
p "# cluster."
p "#"
p "# The CloudFormation controller for Flux automates syncing CloudFormation templates from source"
p "# locations like git repositories into CloudFormation stacks in your AWS account."
p "#"
p "# Let's walk through an example!"
NO_WAIT=false

pe "cd examples/"

p "# I have 2 git repositories here:"

pe "ls -1"

# Highlight repos, press enter to continue

PROMPT_TIMEOUT=0
p "# First, I have a repository that stores the CloudFormation templates that I need to deploy."
PROMPT_TIMEOUT=1

pe "ls -1 my-cloudformation-templates"

# Highlight template files, press enter to continue

PROMPT_TIMEOUT=0
p "# I have three CloudFormation template files that will be deployed to three stacks."

NO_WAIT=true
p "# I also have a git repository that stores the configuration for Flux running in my Kubernetes"
p "# cluster."
NO_WAIT=false

pe "cd my-flux-configuration"
pe "ls -1"

# Highlight the template git repo file, press enter to continue

PROMPT_TIMEOUT=0
p "# I first hooked up my CloudFormation template git repository to Flux."
PROMPT_TIMEOUT=1

NO_WAIT=true
p "# Flux polls my git repository every five minutes to check for new commits to my CloudFormation"
p "# templates."
NO_WAIT=false

pe "cat my-cloudformation-templates-repo.yaml"

# Highlight repo configuration, press enter to continue

PROMPT_TIMEOUT=0
p "# In my Kubernetes cluster, I can see that Flux has the latest commits for my git repositories."
PROMPT_TIMEOUT=1

pe "flux get sources git"

# Highlight git sources, press enter to continue

PROMPT_TIMEOUT=0
p "# In my Flux configuration, I have three Flux CloudFormationStack objects defined."
PROMPT_TIMEOUT=1

pe "ls -1 *-stack.yaml"

NO_WAIT=true
p "# For each CloudFormationStack object, the CloudFormation controller for Flux will create and"
p "# update a CloudFormation stack in my AWS account."
p "#"
p "# The CloudFormationStack configuration specifies which source code repository and file contain"
p "# the template for the stack, and how often to re-sync the latest template into the stack."
NO_WAIT=false

pe "cat my-cloudformation-stack.yaml"

# Highlight stack configuration, press enter to continue

PROMPT_TIMEOUT=0
p "# Let's push this configuration to Flux and watch it create the CloudFormation stacks!"
PROMPT_TIMEOUT=1

cd ../../testdata/my-flux-configuration

pe "git add *-stack.yaml"
pe "git commit -m 'Add CFN stacks'"
pe "git push -q"
pe "flux reconcile source git flux-system"
pe "flux get sources git"

pe "kubectl get cfnstack -A --watch"

pe "kubectl get cfnstack -A"

p "# The stacks are now created in my AWS account!"

# Highlight succeeded reconciliation, press enter to continue

PROMPT_TIMEOUT=0
pe "aws cloudformation describe-stacks --stack-name my-cfn-stack-deployed-by-flux"

# Highlight stack status, press enter to continue

p "# Let's now update a template file and watch Flux automatically deploy the change."
PROMPT_TIMEOUT=1

pe "cd ../my-cloudformation-templates"
pe "sed -i 's/Hello World/Hey there/g' template.yaml"
pe "git diff"
pe "git add template.yaml"
pe "git commit -m 'Update template file'"
pe "git push -q"
pe "flux reconcile source git my-cfn-templates-repo"
pe "flux get sources git"

pe "kubectl get cfnstack -A --watch"

pe "kubectl get cfnstack -A"

p "# The stack is now updated in my AWS account with the latest template file!"

# Highlight stack status, press enter to continue

PROMPT_TIMEOUT=0
pe "aws cloudformation describe-stacks --stack-name my-cfn-stack-deployed-by-flux"

p "# Enjoy continuous delivery of your CloudFormation stacks with Flux!"
