Feature: CloudFormation controller for Flux
  A Flux user can automatically sync a CloudFormation template stored
  in a git repository into a CloudFormation stack in their AWS account.

  Background:
    Given I have a Kubernetes cluster with the CloudFormation controller for Flux installed
    And I have a git repository for my CloudFormation template files registered with Flux

  Scenario: New CloudFormation stack
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository

    When I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      stackName: {stack_name}
      templatePath: {template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
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
      stackName: {stack_name}
      templatePath: {template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      stackName: {stack_name}
      templatePath: {other_template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
      """

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "UPDATE_COMPLETE" state

  Scenario: Update an existing CloudFormation stack by pushing a template file change
    Given I push a valid CloudFormation template to my git repository
    And I trigger Flux to reconcile my git repository
    And I apply the following CloudFormationStack configuration to my Kubernetes cluster
      """
      stackName: {stack_name}
      templatePath: {template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
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
      stackName: {stack_name}
      templatePath: {template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
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
      stackName: {stack_name}
      templatePath: {template_path}
      sourceRef:
        kind: GitRepository
        name: integ-test-cfn-templates-repo
      interval: 1h
      retryInterval: 5s
      destroyStackOnDeletion: true
      """
    And the CloudFormationStack's Ready condition should eventually have "True" status
    And the CloudFormation stack in my AWS account should be in "CREATE_COMPLETE" state

    When I mark the CloudFormationStack for deletion

    Then the CloudFormationStack's Ready condition should eventually have "Unknown" status
    And the CloudFormationStack should eventually be deleted
    And the CloudFormation stack in my AWS account should be deleted
