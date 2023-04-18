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

    Then the CloudFormationStack's Ready condition should eventually have "False" status
