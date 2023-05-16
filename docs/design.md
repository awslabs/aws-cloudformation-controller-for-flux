# AWS CloudFormation Template Sync Controller for Flux Design

<!-- toc -->

1. [Overview](#overview)
1. [Sequence of events for CloudFormation stack creation](#sequence-of-events-for-cloudformation-stack-creation)
1. [Sequence of events for CloudFormation stack update](#sequence-of-events-for-cloudformation-stack-update)
1. [Sequence of events for CloudFormation stack deletion](#sequence-of-events-for-cloudformation-stack-deletion)
1. [Reconciliation logic](#reconciliation-logic)
<!-- tocstop -->

## Overview

The CloudFormation controller registers a `CloudFormationStack` custom resource type into the Kubernetes API server
(see the [CloudFormationStack API reference](./api/cloudformationstack.md)).
The CloudFormationStack resource type describes the specifications for a CloudFormation stack that the CloudFormation
controller should manage, including the location of a CloudFormation template file in a source code repository. Users
create CloudFormation stack objects in their Kubernetes cluster using kubectl to register CloudFormation stacks that
they want Flux to manage. They can also describe these objects using kubectl to get the latest status of the stack,
like whether the latest source code changes have been synced to the stack by Flux. The CloudFormation controller
listens for changes to objects of this type (like creations or updates) from the API server, syncs templates from
source code into the desired CloudFormation stacks, and stores the latest status back into the CloudFormationStack
objects by sending updates to the API server.

## Sequence of events for CloudFormation stack creation

![Create flow](./diagrams/data-flow-create.png 'Create flow')

1. The Kubernetes user registers a git repository with Flux by creating a GitRepository object through the Kubernetes API.
2. The Flux source controller watches the Kubernetes API for GitRepository object changes, and is notified that a new GitRepository object was created.
3. The Flux source controller begins polling the git repository for new git commits.
4. The git user pushes a CloudFormation template file to the git repository.
5. The Flux source controller detects the new git commit containing the CloudFormation template file during the next poll of the git repository. It clones the git repository to its local disk and creates a tarball artifact of the repository contents.
6. The Flux source controller updates the GitRepository object through the Kubernetes API with the git commit ID it cloned and information about the artifact it created from the repository contents.
7. The Kubernetes user creates a CloudFormationStack object through the Kubernetes API.
8. The CloudFormation controller watches the Kubernetes API for changes to CloudFormationStack objects, and is notified that a new CloudFormationStack object was created.
9. The CloudFormation controller retrieves the GitRepository object from the Kubernetes API that the CloudFormationStack object refers to as the source of the stack template file.
10. The CloudFormation controller downloads the contents of the source code repository from the Flux source controller using the artifact URL in the retrieved GitRepository object.
11. The CloudFormation controller uploads the template file from the source code repository to S3.
12. The CloudFormation controller deploys the template file from S3 to a CloudFormation stack.
13. During the stack deployment, the CloudFormation controller updates the status fields in the CloudFormationStack object through the Kubernetes API, eventually marking the stack as successfully deployed.

## Sequence of events for CloudFormation stack update

![Update flow](./diagrams/data-flow-update.png 'Update flow')

1. The git user pushes an update to the CloudFormation template file to the git repository.
2. The Flux source controller detects the new git commit during the next poll of the git repository. It clones the git repository to its local disk and creates a tarball artifact of the repository contents.
3. The Flux source controller updates the GitRepository object through the Kubernetes API with the git commit ID it cloned and information about the artifact it created from the repository contents.
4. The CloudFormation controller watches the Kubernetes API for changes to the GitRepository object referred to by the CloudFormationStack object, and is notified that the GitRepository object was updated.
5. The CloudFormation controller retrieves the CloudFormationStack object that refers to the updated GitRepository object from the Kubernetes API.
6. The CloudFormation controller downloads the new contents of the source code repository from the Flux source controller using the artifact URL in the retrieved GitRepository object.
7. The CloudFormation controller uploads the updated template file from the source code repository to S3.
8. The CloudFormation controller deploys the template file from S3 to the CloudFormation stack.
9. During the stack deployment, the CloudFormation controller updates the status fields in the CloudFormationStack object through the Kubernetes API, eventually marking the stack as successfully updated.

## Sequence of events for CloudFormation stack deletion

![Delete flow](./diagrams/data-flow-delete.png 'Delete flow')

1. The Kubernetes user marks the CloudFormationStack object for deletion through the Kubernetes API.
2. The CloudFormation controller watches the Kubernetes API for changes to CloudFormationStack objects, and is notified that a new CloudFormationStack object was marked for deletion.
3. The CloudFormation controller deletes the CloudFormation stack.
4. During stack deletion, the CloudFormation controller updates the status fields in the CloudFormationStack object through the Kubernetes API, eventually marking the stack as successfully deleted. When the CloudFormationStack object status field shows successful stack deletion, the Kubernetes API server fully deletes the CloudFormationStack object.

## Reconciliation logic

The CloudFormation controller continuously reconciles the CloudFormationStack objects with the real CloudFormation
stacks in your AWS account. On a regular interval defined by the user, the CloudFormation controller takes actions to
move the real CloudFormation stack to match the desired state (i.e. the template) defined by the CloudFormationStack
object in Kubernetes. For example, a reconciliation loop may create a new stack or execute a stack change set in your
AWS account to ensure the stack is deployed with the latest template. When the CloudFormation controller starts up, it
lists all CloudFormationStack objects in the Kubernetes cluster, and runs a reconciliation loop on each object. The
controller then adds the stack objects to an internal in-memory queue with a delay defined by each stack’s
reconciliation interval.

Stack objects can define faster reconciliation intervals for certain cases. They can define a "polling" interval to run
a reconciliation loop more often when a stack action is in progress and needs to be checked on frequently, for example
stack creation or change set creation. They can also define a "retry" interval to quickly retry failed reconciliation
loops, for example if an API call to CloudFormation failed or if a change set failed to execute.

The following diagram shows the logic of a single reconciliation loop, starting from describing the current state of
the stack to creating or executing change sets to apply the latest template to the stack.

![Reconciliation loop](./diagrams/reconciliation-loop.png 'Reconciliation loop')

When the CloudFormationStack object has been marked for deletion from the Kubernetes API server by a user, the
CloudFormation controller follows different reconciliation logic to delete the real CloudFormation stack in your AWS
account.

![Deletion reconciliation loop](./diagrams/reconciliation-loop-deletion.png 'Deletion reconciliation loop')
