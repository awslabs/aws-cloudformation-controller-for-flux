//go:build integration

// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT-0

package integtests

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/smithy-go"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
)

const (
	ValidCfnTemplateFile          = "examples/my-cloudformation-templates/template.yaml"
	AnotherValidCfnTemplateFile   = "examples/my-cloudformation-templates/another-template.yaml"
	ThirdValidCfnTemplateFile     = "examples/my-cloudformation-templates/yet-another-template.yaml"
	CfnTemplateFileWithParameters = "examples/my-cloudformation-templates/template-with-parameters.yaml"

	EventuallyMaxAttempts = 300
	EventuallyRetryDelay  = "1s"
)

type cfnControllerScenario struct {
	suite *cfnControllerTestSuite

	// Information about the CloudFormation templates git repository
	cfnTemplateFile               string
	otherCfnTemplateFile          string
	cfnTemplateFileWithParameters string

	// Information about the CloudFormation stacks
	realCfnStackName        string
	cfnStackObjectName      string
	otherRealCfnStackName   string
	otherCfnStackObjectName string

	cleanedUp bool
}

// After the scenario is completed:
// 1. Delete the scenario's CFN stack object from the cluster
// 2. Delete the scenario's CFN stack
// 3. Delete the scenario's CFN templates from the template git repo
func (s *cfnControllerScenario) cleanup(ctx context.Context) error {
	if s.cleanedUp {
		return nil
	}

	// Delete the CFN stack object from the cluster
	if s.cfnStackObjectName != "" {
		if err := s.suite.cmdRunner.run("kubectl", "delete", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system", "--ignore-not-found=true"); err != nil {
			return err
		}
	}

	if s.otherCfnStackObjectName != "" {
		if err := s.suite.cmdRunner.run("kubectl", "delete", "cfnstack", s.otherCfnStackObjectName, "--namespace", "flux-system", "--ignore-not-found=true"); err != nil {
			return err
		}
	}

	// Delete the real CFN stack
	if s.realCfnStackName != "" {
		_, err := s.suite.cfnClient.DeleteStack(ctx, &cloudformation.DeleteStackInput{
			StackName: aws.String(s.realCfnStackName),
		})
		if err != nil && !stackDoesNotExist(err) {
			return err
		}
	}

	if s.otherRealCfnStackName != "" {
		_, err := s.suite.cfnClient.DeleteStack(ctx, &cloudformation.DeleteStackInput{
			StackName: aws.String(s.otherRealCfnStackName),
		})
		if err != nil && !stackDoesNotExist(err) {
			return err
		}
	}

	// Delete the CFN templates from the templates git repo
	deleteFilesFromGitRepository(
		ctx,
		CfnTemplateRepoName,
		s.suite.gitCredentials,
		s.cfnTemplateFile,
		s.otherCfnTemplateFile,
		s.cfnTemplateFileWithParameters)

	s.cleanedUp = true
	return nil
}

// Steps that manipulate the template git repository

func (s *cfnControllerScenario) createCfnTemplateFile(ctx context.Context) error {
	newFilePath, err := copyFileToGitRepository(ctx, CfnTemplateRepoName, s.suite.gitCredentials, ValidCfnTemplateFile, "")
	s.cfnTemplateFile = newFilePath
	if err != nil {
		return err
	}
	return nil
}

func (s *cfnControllerScenario) createSecondCfnTemplateFile(ctx context.Context) error {
	newFilePath, err := copyFileToGitRepository(ctx, CfnTemplateRepoName, s.suite.gitCredentials, AnotherValidCfnTemplateFile, "")
	s.otherCfnTemplateFile = newFilePath
	if err != nil {
		return err
	}
	return nil
}

func (s *cfnControllerScenario) createCfnTemplateFileWithParameters(ctx context.Context) error {
	newFilePath, err := copyFileToGitRepository(ctx, CfnTemplateRepoName, s.suite.gitCredentials, CfnTemplateFileWithParameters, "")
	s.cfnTemplateFileWithParameters = newFilePath
	if err != nil {
		return err
	}
	return nil
}

func (s *cfnControllerScenario) updateCfnTemplateFile(ctx context.Context) error {
	_, err := copyFileToGitRepository(ctx, CfnTemplateRepoName, s.suite.gitCredentials, ThirdValidCfnTemplateFile, s.cfnTemplateFile)
	if err != nil {
		return err
	}
	return nil
}

// Steps that manage CloudFormationStack objects in Kubernetes

func (s *cfnControllerScenario) applyCfnStackConfiguration(cfnStackSpec *godog.DocString) error {
	// Example
	/**
	  apiVersion: cloudformation.contrib.fluxcd.io/v1alpha1
	  kind: CloudFormationStack
	  metadata:
	    name: {stack_object_name}
	    namespace: flux-system
	  spec:
	    stackName: {stack_name}
	    templatePath: {template_path} (or {other_template_path})
	    sourceRef:
	      kind: GitRepository
	      name: my-cfn-templates-repo
	    interval: 1h
	    retryInterval: 5s
	*/

	if s.cfnStackObjectName == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		s.cfnStackObjectName = fmt.Sprintf("integ-test-cfnstack-%s", id.String())
		s.realCfnStackName = fmt.Sprintf("cfn-flux-controller-integ-test-%s", id.String())
	}

	stackSpec := cfnStackSpec.Content
	stackSpec = strings.Replace(stackSpec, "{stack_name}", s.realCfnStackName, -1)
	stackSpec = strings.Replace(stackSpec, "{stack_object_name}", s.cfnStackObjectName, -1)

	if strings.Contains(stackSpec, "{other_stack_object_name}") {
		if s.otherCfnStackObjectName == "" {
			id, err := uuid.NewRandom()
			if err != nil {
				return err
			}
			s.otherCfnStackObjectName = fmt.Sprintf("integ-test-cfnstack-%s", id.String())
			s.otherRealCfnStackName = fmt.Sprintf("cfn-flux-controller-integ-test-%s", id.String())
		}
		stackSpec = strings.Replace(stackSpec, "{other_stack_name}", s.otherRealCfnStackName, -1)
		stackSpec = strings.Replace(stackSpec, "{other_stack_object_name}", s.otherCfnStackObjectName, -1)
	}

	if s.cfnTemplateFile != "" {
		stackSpec = strings.Replace(stackSpec, "{template_path}", s.cfnTemplateFile, -1)
	}

	if s.cfnTemplateFileWithParameters != "" {
		stackSpec = strings.Replace(stackSpec, "{template_with_parameters_path}", s.cfnTemplateFileWithParameters, -1)
	}

	if s.otherCfnTemplateFile != "" {
		stackSpec = strings.Replace(stackSpec, "{other_template_path}", s.otherCfnTemplateFile, -1)
	}

	s.suite.cmdRunner.runWithStdIn(stackSpec, "kubectl", "apply", "-f", "-")

	return s.suite.cmdRunner.runWithStdIn(stackSpec, "kubectl", "apply", "-f", "-")
}

func (s *cfnControllerScenario) deleteCfnStackObject() error {
	return s.suite.cmdRunner.run("kubectl", "delete", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system")
}

func (s *cfnControllerScenario) cfnStackObjectShouldBeDeleted() error {
	rootDir, err := filepath.Abs("../..")
	if err != nil {
		s.suite.testingT.Error(err)
		return err
	}

	eventuallyErr := eventually(func() error {
		cmd := exec.Command("kubectl", "get", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system")
		cmd.Dir = rootDir
		_, err := cmd.Output()

		if err == nil {
			return errors.New(fmt.Sprintf("CloudFormationStack object %s still exists", s.cfnStackObjectName))
		}

		if ee, ok := err.(*exec.ExitError); ok {
			errMsg := string(ee.Stderr)
			if strings.Contains(errMsg, "NotFound") {
				return nil
			}
		}

		return err
	})

	if eventuallyErr != nil {
		output, err := s.suite.cmdRunner.getOutput("kubectl", "get", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system")
		if err == nil {
			s.suite.testingT.Error(output)
		}
		return eventuallyErr
	}
	return nil
}

func (s *cfnControllerScenario) cfnStackObjectShouldHaveStatus(expectedStatus string) error {
	eventuallyErr := eventually(func() error {
		out, err := s.suite.cmdRunner.getOutput("kubectl", "get", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system", "-o", "jsonpath=\"{.status.conditions[?(@.type=='Ready')].status}\"")
		if err != nil {
			return err
		}
		out = strings.Trim(out, "\"")

		if out == expectedStatus {
			return nil
		}
		return errors.New(fmt.Sprintf("CloudFormationStack object %s did not achieve expected status Ready=%s, instead Ready=%s", s.cfnStackObjectName, expectedStatus, out))
	})

	if eventuallyErr != nil {
		output, err := s.suite.cmdRunner.getOutput("kubectl", "get", "cfnstack", s.cfnStackObjectName, "--namespace", "flux-system")
		if err == nil {
			s.suite.testingT.Error(output)
		}
		return eventuallyErr
	}
	return nil
}

func (s *cfnControllerScenario) otherCfnStackObjectShouldHaveStatus(expectedStatus string) error {
	eventuallyErr := eventually(func() error {
		out, err := s.suite.cmdRunner.getOutput("kubectl", "get", "cfnstack", s.otherCfnStackObjectName, "--namespace", "flux-system", "-o", "jsonpath=\"{.status.conditions[?(@.type=='Ready')].status}\"")
		if err != nil {
			return err
		}
		out = strings.Trim(out, "\"")

		if out == expectedStatus {
			return nil
		}
		return errors.New(fmt.Sprintf("CloudFormationStack object %s did not achieve expected status Ready=%s, instead Ready=%s", s.otherCfnStackObjectName, expectedStatus, out))
	})

	if eventuallyErr != nil {
		output, err := s.suite.cmdRunner.getOutput("kubectl", "get", "cfnstack", s.otherCfnStackObjectName, "--namespace", "flux-system")
		if err == nil {
			s.suite.testingT.Error(output)
		}
		return eventuallyErr
	}
	return nil
}

// Steps that manage real CloudFormation stacks

func (s *cfnControllerScenario) realCfnStackShouldBeDeleted(ctx context.Context) error {
	out, err := s.suite.cfnClient.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(s.realCfnStackName),
	})
	if err != nil {
		if stackDoesNotExist(err) {
			return nil
		}
		return err
	}
	if len(out.Stacks) == 0 {
		return nil
	}
	if out.Stacks[0].StackStatus == types.StackStatusDeleteComplete {
		return nil
	}

	return errors.New(fmt.Sprintf("CloudFormation stack %s is not deleted, current status %s", s.realCfnStackName, out.Stacks[0].StackStatus))
}

func (s *cfnControllerScenario) realCfnStackShouldBeInState(ctx context.Context, expectedState string) error {
	out, err := s.suite.cfnClient.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: aws.String(s.realCfnStackName),
	})
	if err != nil {
		return err
	}
	if len(out.Stacks) == 0 {
		return errors.New(fmt.Sprintf("Could not find CloudFormation stack %s", s.realCfnStackName))
	}

	if string(out.Stacks[0].StackStatus) != expectedState {
		return errors.New(fmt.Sprintf("CloudFormation stack %s is not in expected state %s, current state is %s", s.realCfnStackName, expectedState, out.Stacks[0].StackStatus))
	}

	return nil
}

// Retry a function with a constant delay up to a max number of attempts
func eventually(f func() error) (err error) {
	delay, durationErr := time.ParseDuration(EventuallyRetryDelay)
	if err != nil {
		return durationErr
	}

	for i := 0; i < EventuallyMaxAttempts; i++ {
		err = f()
		if err != nil {
			time.Sleep(delay)
			continue
		}
		break
	}
	return err
}

func stackDoesNotExist(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "ValidationError":
			// A ValidationError occurs if we describe a stack which doesn't exist.
			if strings.Contains(ae.ErrorMessage(), "does not exist") {
				return true
			}
		}
	}
	return false
}
