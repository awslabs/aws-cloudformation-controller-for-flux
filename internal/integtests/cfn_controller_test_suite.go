//go:build integration

package integtests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/cucumber/godog"
	git "github.com/go-git/go-git/v5"
	http "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	GitCredentialsSecretName  = "flux-git-credentials"
	GitCredentialsUserNameKey = "ServiceUserName"
	GitCredentialsPasswordKey = "ServicePassword"

	FluxConfigRepoName         = "my-flux-configuration"
	CfnTemplatesRepoConfigFile = "examples/my-flux-configuration/my-cloudformation-templates-repo.yaml"
	CfnTemplateRepoName        = "my-cloudformation-templates"
)

type cfnControllerTestSuite struct {
	skipClusterBootstrap bool
	testingT             *testing.T
	cmdRunner            *cfnControllerTestCommandRunner
	secretsManagerClient *secretsmanager.Client
	cfnClient            *cloudformation.Client
	gitCredentials       *http.BasicAuth

	// Information about the flux configuration git repository
	fluxConfigRepo                   *git.Repository
	fluxConfigRepoDir                string
	fluxConfigTemplateRepoConfigFile string

	// Information about the CloudFormation templates git repository
	cfnTemplateRepo    *git.Repository
	cfnTemplateRepoDir string
}

func (t *cfnControllerTestSuite) InitializeTestSuite(ctx *godog.TestSuiteContext) {
	// Before starting the test suite:
	// 1. Bootstrap and validate the local Kubernetes cluster
	// 2. Clone the Flux config git repo and CloudFormation template git repo locally
	// 3. Register the CFN template git repo with Flux
	ctx.BeforeSuite(func() {
		if !t.skipClusterBootstrap {
			// Bootstrap the local cluster
			t.testingT.Log("Bootstrapping the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("make", "bootstrap-local-cluster")
			t.testingT.Log("Deploying the CloudFormation controller to the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("make", "deploy")
		}

		resp, err := t.secretsManagerClient.GetSecretValue(context.TODO(), &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(GitCredentialsSecretName),
		})
		if err != nil {
			t.testingT.Error(err)
			t.testingT.FailNow()
		}

		creds := map[string]string{}
		json.Unmarshal([]byte(*resp.SecretString), &creds)

		auth := &http.BasicAuth{
			Username: creds[GitCredentialsUserNameKey],
			Password: creds[GitCredentialsPasswordKey],
		}
		t.gitCredentials = auth

		if err = t.checkKubernetesCluster(); err != nil {
			t.testingT.Error(err)
			t.testingT.FailNow()
		}
		if err = t.checkTemplateGitRepository(context.TODO()); err != nil {
			t.testingT.Error(err)
			t.testingT.FailNow()
		}

		// TODO clear out any old CFN stacks from previous integ tests
	})

	// After completing the test suite:
	// 1. Remove the local git repos from disk
	// 2. Tear down the local Kubernetes cluster
	ctx.AfterSuite(func() {
		t.cleanup()
		if !t.skipClusterBootstrap {
			// Tear down the local cluster
			t.testingT.Log("Tearing down the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("kind", "delete", "cluster")
		}
	})
}

func (t *cfnControllerTestSuite) InitializeScenario(ctx *godog.ScenarioContext) {
	scenario := &cfnControllerScenario{
		suite: t,
	}

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		cleanupErr := scenario.cleanup(ctx)
		return ctx, cleanupErr
	})

	ctx.Step(`^I apply the following CloudFormationStack configuration to my Kubernetes cluster$`, scenario.applyCfnStackConfiguration)
	ctx.Step(`^I mark the CloudFormationStack for deletion$`, scenario.deleteCfnStackObject)
	ctx.Step(`^I push a valid CloudFormation template to my git repository$`, scenario.createCfnTemplateFile)
	ctx.Step(`^I push an update for my CloudFormation template to my git repository$`, scenario.updateCfnTemplateFile)
	ctx.Step(`^I push another valid CloudFormation template to my git repository$`, scenario.createSecondCfnTemplateFile)
	ctx.Step(`^I trigger Flux to reconcile my git repository$`, t.reconcileTemplateGitRepository)
	ctx.Step(`^the CloudFormation stack in my AWS account should be deleted$`, scenario.realCfnStackShouldBeDeleted)
	ctx.Step(`^the CloudFormation stack in my AWS account should be in "([^"]*)" state$`, scenario.realCfnStackShouldBeInState)
	ctx.Step(`^the CloudFormationStack should eventually be deleted$`, scenario.cfnStackObjectShouldBeDeleted)
	ctx.Step(`^the CloudFormationStack\'s Ready condition should eventually have "([^"]*)" status$`, scenario.cfnStackObjectShouldHaveStatus)
}

func (s *cfnControllerTestSuite) cleanup() {
	if s.fluxConfigRepoDir != "" {
		os.RemoveAll(s.fluxConfigRepoDir)
	}
	if s.cfnTemplateRepoDir != "" {
		os.RemoveAll(s.cfnTemplateRepoDir)
	}
}

func (s *cfnControllerTestSuite) checkTemplateGitRepository(ctx context.Context) error {
	// Clone the Flux config repository locally, and add the template git repository configuration to the Flux config repo
	repo, dir, err := cloneGitRepository(ctx, FluxConfigRepoName, s.gitCredentials)
	s.fluxConfigRepo = repo
	s.fluxConfigRepoDir = dir
	if err != nil {
		return err
	}

	// Add the template git repository configuration to the Flux config repo
	s.fluxConfigTemplateRepoConfigFile = filepath.Join(s.fluxConfigRepoDir, "my-cloudformation-templates-repo.yaml")
	_, err = copyFileToGitRepository(dir, repo, s.gitCredentials, CfnTemplatesRepoConfigFile, s.fluxConfigTemplateRepoConfigFile)
	if err != nil {
		return err
	}

	// Validate that Flux can pull from the Flux config repo
	if err = s.reconcileFluxConfigGitRepository(); err != nil {
		return err
	}

	// Validate that Flux can pull from the CFN templates repo
	if err = s.reconcileTemplateGitRepository(); err != nil {
		return err
	}

	// Clone the template git repository locally
	// TODO clear out any old integ test templates
	repo, dir, err = cloneGitRepository(ctx, CfnTemplateRepoName, s.gitCredentials)
	s.cfnTemplateRepo = repo
	s.cfnTemplateRepoDir = dir
	if err != nil {
		return err
	}

	return nil
}

func (s *cfnControllerTestSuite) checkKubernetesCluster() error {
	if err := s.cmdRunner.run("kubectl", "version"); err != nil {
		return err
	}
	if err := s.cmdRunner.run("flux", "check"); err != nil {
		return err
	}
	out, err := s.cmdRunner.getOutput("kubectl", "get", "deployment", "cfn-controller", "--namespace", "flux-system", "-o", "jsonpath=\"{.status.conditions[?(@.type == 'Available')].status}\"")
	if err != nil {
		return err
	}
	out = strings.Trim(out, "\"")

	if out != "True" {
		return errors.New("CloudFormation controller is not available in the Kubernetes cluster, current Available status is " + out)
	}
	return nil
}

func (s *cfnControllerTestSuite) reconcileFluxConfigGitRepository() error {
	if err := s.cmdRunner.run("flux", "reconcile", "source", "git", "flux-system"); err != nil {
		return err
	}
	out, err := s.cmdRunner.getOutput("flux", "get", "sources", "git", "flux-system", "--status-selector", "ready=true", "--no-header")
	if err != nil {
		return err
	}
	if out == "" {
		output, err := s.cmdRunner.getOutput("flux", "get", "sources", "git", "flux-system")
		if err == nil {
			s.testingT.Error(output)
		}
		return errors.New("CloudFormation template file repository could not be reconciled by Flux")
	}
	return nil
}

func (s *cfnControllerTestSuite) reconcileTemplateGitRepository() error {
	if err := s.cmdRunner.run("flux", "reconcile", "source", "git", "my-cfn-templates-repo"); err != nil {
		return err
	}
	out, err := s.cmdRunner.getOutput("flux", "get", "sources", "git", "my-cfn-templates-repo", "--status-selector", "ready=true", "--no-header")
	if err != nil {
		return err
	}
	if out == "" {
		output, err := s.cmdRunner.getOutput("flux", "get", "sources", "git", "my-cfn-templates-repo")
		if err == nil {
			s.testingT.Error(output)
		}
		return errors.New("CloudFormation template file repository could not be reconciled by Flux")
	}
	return nil
}
