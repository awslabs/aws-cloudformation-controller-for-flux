Feature: CloudFormation controller for Flux
  A Flux user can automatically sync a CloudFormation template stored
  in a git repository into a CloudFormation stack in their AWS account.

  Scenario: New CloudFormation stack
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository

    When I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
      """

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

  Scenario: Update an existing CloudFormation stack by changing the stack object configuration
    Given I push a valid CloudFormation template to my git repository
    And I push another valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository
    And I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {other_template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
      """

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "UPDATE_COMPLETE" state

  Scenario: Update an existing CloudFormation stack by pushing a template file change
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository
    And I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I push an update for my CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "UPDATE_COMPLETE" state

  Scenario: Reconcile an existing CloudFormation stack with no changes needed
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository
    And I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I push another valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

  Scenario: Delete a CloudFormation stack
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository
    And I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
      kind: CloudFormationStack
      metadata:
        name: {stack_object_name}
        namespace: flux-system
      spec:
        stackName: {stack_name}
        templatePath: {template_path}
        sourceRef:
          kind: GitRepository
          name: my-cfn-templates-repo
        interval: 1h
        retryInterval: 5s
        destroyStackOnDeletion: true
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I mark the CloudFormationStack for deletion

    Then the CloudFormationStack should eventually be deleted
    And the CloudFormation stack in my AWS account should be deleted
