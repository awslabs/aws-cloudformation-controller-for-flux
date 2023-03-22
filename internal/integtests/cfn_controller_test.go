//go:build integration

package integtests

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	git "github.com/go-git/go-git/v5"
	http "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	Region                    = "us-west-2"
	FluxConfigRepoName        = "my-flux-configuration"
	CfnTemplateRepoName       = "my-cloudformation-templates"
	GitCredentialsSecretName  = "flux-git-credentials"
	GitCredentialsUserNameKey = "ServiceUserName"
	GitCredentialsPasswordKey = "ServicePassword"
)

var opts = godog.Options{Output: colors.Colored(os.Stdout)}
var skipClusterBootstrap = flag.Bool("skip-cluster-bootstrap", false, "run integration tests against an existing cluster (do not bootstrap a new cluster)")

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &opts)
}

func TestCloudFormationController(t *testing.T) {
	testSuite := &cfnControllerTestSuite{
		testingT: t,
		cmdRunner: &cfnControllerTestCommandRunner{
			testingT:  t,
			stdLogger: &cfnControllerTestStdLogger{testingT: t},
			errLogger: &cfnControllerTestErrLogger{testingT: t},
		},
	}

	o := opts
	o.TestingT = t

	status := godog.TestSuite{
		Name:                 "flux",
		Options:              &o,
		TestSuiteInitializer: testSuite.InitializeTestSuite,
		ScenarioInitializer:  testSuite.InitializeScenario,
	}.Run()

	if status == 2 {
		t.SkipNow()
	}

	if status != 0 {
		t.Fatalf("zero status code expected, %d received", status)
	}
}

type cfnControllerTestSuite struct {
	testingT  *testing.T
	cmdRunner *cfnControllerTestCommandRunner
}

// Use custom writers so that we can pipe command output to the testing framework's logger
type cfnControllerTestCommandRunner struct {
	testingT  *testing.T
	stdLogger *cfnControllerTestStdLogger
	errLogger *cfnControllerTestErrLogger
}

type cfnControllerTestStdLogger struct {
	testingT *testing.T
}

func (l *cfnControllerTestStdLogger) Write(data []byte) (n int, err error) {
	l.testingT.Log(string(data))
	return len(data), err
}

type cfnControllerTestErrLogger struct {
	testingT *testing.T
}

func (l *cfnControllerTestErrLogger) Write(data []byte) (n int, err error) {
	l.testingT.Error(string(data))
	return len(data), err
}

func (t *cfnControllerTestCommandRunner) runExitOnFail(command string, arg ...string) {
	// current working directory is expected to be <root>/internal/integtests
	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.testingT.Error(err)
		t.testingT.FailNow()
	}

	cmd := exec.Command(command, arg...)
	cmd.Dir = rootDir
	cmd.Stdout = t.stdLogger
	cmd.Stderr = t.errLogger
	t.testingT.Log(fmt.Sprintf("Running command %s %s", command, strings.Join(arg, " ")))
	if err := cmd.Run(); err != nil {
		t.testingT.Error(err)
		t.testingT.FailNow()
	}
}

func (t *cfnControllerTestCommandRunner) run(command string, arg ...string) error {
	// current working directory is expected to be <root>/internal/integtests
	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.testingT.Error(err)
		return err
	}

	cmd := exec.Command(command, arg...)
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		t.testingT.Error(fmt.Sprintf("Command failed: %s %s", command, strings.Join(arg, " ")))
		t.testingT.Log(output)
		t.testingT.Error(err)
		return err
	}
	return nil
}

func (t *cfnControllerTestCommandRunner) getOutput(command string, arg ...string) (string, error) {
	// current working directory is expected to be <root>/internal/integtests
	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.testingT.Error(err)
		return "", err
	}

	cmd := exec.Command(command, arg...)
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		t.testingT.Error(fmt.Sprintf("Command failed: %s %s", command, strings.Join(arg, " ")))
		t.testingT.Error(err)
		return "", err
	}

	return string(output), nil
}

func (t *cfnControllerTestSuite) InitializeTestSuite(ctx *godog.TestSuiteContext) {
	ctx.BeforeSuite(func() {
		if !*skipClusterBootstrap {
			// Bootstrap the local cluster
			t.testingT.Log("Bootstrapping the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("make", "bootstrap-local-cluster")
			t.testingT.Log("Deploying the CloudFormation controller to the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("make", "deploy")
		} else {
			t.testingT.Log("Skipping bootstrapping a local Kubernetes cluster")
		}
	})
	ctx.AfterSuite(func() {
		if !*skipClusterBootstrap {
			// Tear down the local cluster
			t.testingT.Log("Tearing down the local Kubernetes cluster")
			t.cmdRunner.runExitOnFail("kind", "delete", "cluster")
		}
	})
}

func (t *cfnControllerTestSuite) InitializeScenario(ctx *godog.ScenarioContext) {
	scenario := &cfnControllerScenario{
		testingT:  *t.testingT,
		cmdRunner: t.cmdRunner,
	}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		// TODO fill in
		return ctx, nil
	})

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		cleanupErr := scenario.cleanup()
		return ctx, cleanupErr
	})

	ctx.Step(`^I have a Kubernetes cluster with the CloudFormation controller for Flux installed$`, scenario.checkKubernetesCluster)
	ctx.Step(`^I have a git repository for my CloudFormation template files registered with Flux$`, scenario.checkTemplateGitRepository)
	ctx.Step(`^I apply the following CloudFormationStack configuration to my Kubernetes cluster$`, scenario.applyCfnStackConfiguration)
	ctx.Step(`^I mark the CloudFormationStack for deletion$`, scenario.deleteCfnStackObject)
	ctx.Step(`^I push a valid CloudFormation template to my git repository$`, scenario.createCfnTemplateFile)
	ctx.Step(`^I push an update for my CloudFormation template to my git repository$`, scenario.updateCfnTemplateFile)
	ctx.Step(`^I push another valid CloudFormation template to my git repository$`, scenario.createSecondCfnTemplateFile)
	ctx.Step(`^I trigger Flux to reconcile my git repository$`, scenario.reconcileGitRepository)
	ctx.Step(`^the CloudFormation stack in my AWS account should be deleted$`, scenario.realCfnStackShouldBeDeleted)
	ctx.Step(`^the CloudFormation stack in my AWS account should be in "([^"]*)" state$`, scenario.realCfnStackShouldBeInState)
	ctx.Step(`^the CloudFormationStack should eventually be deleted$`, scenario.cfnStackObjectShouldBeDeleted)
	ctx.Step(`^the CloudFormationStack\'s Ready condition should eventually have "([^"]*)" status$`, scenario.cfnStackObjectShouldHaveStatus)
}

type cfnControllerScenario struct {
	testingT       testing.T
	cmdRunner      *cfnControllerTestCommandRunner
	gitCredentials *http.BasicAuth

	// Information about the flux configuration git repository
	fluxConfigRepo                   *git.Repository
	fluxConfigRepoDir                string
	fluxConfigTemplateRepoConfigFile string

	// Information about the CloudFormation templates git repository
	cfnTemplateRepo    *git.Repository
	cfnTemplateRepoDir string

	cleanedUp bool
}

// Clones a repository from CodeCommit.
// Returns the temporary directory where the git repository is located
func (s *cfnControllerScenario) cloneGitRepository(ctx context.Context, repositoryName string) (*git.Repository, string, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(Region))
	if err != nil {
		return nil, "", err
	}

	client := secretsmanager.NewFromConfig(cfg)
	resp, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(GitCredentialsSecretName),
	})
	if err != nil {
		return nil, "", err
	}

	creds := map[string]string{}
	json.Unmarshal([]byte(*resp.SecretString), &creds)

	auth := &http.BasicAuth{
		Username: creds[GitCredentialsUserNameKey],
		Password: creds[GitCredentialsPasswordKey],
	}
	s.gitCredentials = auth

	tmpDir, err := os.MkdirTemp("", repositoryName)
	if err != nil {
		return nil, tmpDir, fmt.Errorf("unable to create temp dir for repository %s, error: %w", repositoryName, err)
	}

	repositoryUrl := fmt.Sprintf("https://git-codecommit.%s.amazonaws.com/v1/repos/%s", Region, repositoryName)
	r, err := git.PlainClone(tmpDir, false, &git.CloneOptions{
		URL:  repositoryUrl,
		Auth: auth,
	})
	if err != nil {
		return r, tmpDir, err
	}
	return r, tmpDir, nil
}

// Copies the given file into the git repository using a new unique file name
func (s *cfnControllerScenario) copyFileToGitRepository(dir string, repo *git.Repository, originalFile string) (string, error) {
	content, err := os.ReadFile("../../" + originalFile)
	if err != nil {
		return "", err
	}
	return s.addFileToGitRepository(dir, repo, string(content))
}

// Adds a file with the given content into the git repository using a new unique file name
func (s *cfnControllerScenario) addFileToGitRepository(dir string, repo *git.Repository, content string) (string, error) {
	// Write the file into the git repo on disk
	newFile, err := os.CreateTemp(dir, "integ-test.*.yaml")
	if err != nil {
		return "", err
	}
	newFilePath := newFile.Name()
	if _, err = newFile.Write([]byte(content)); err != nil {
		return newFilePath, err
	}
	if err = newFile.Close(); err != nil {
		return newFilePath, err
	}

	// Add the file to git
	w, err := repo.Worktree()
	if err != nil {
		return newFilePath, err
	}
	if err = w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return newFilePath, err
	}
	if _, err = w.Commit("Add file for integ test", &git.CommitOptions{}); err != nil {
		return newFilePath, err
	}
	if err = repo.Push(&git.PushOptions{RemoteName: "origin", Auth: s.gitCredentials}); err != nil {
		return newFilePath, err
	}
	return newFilePath, nil
}

// Deletes files from the git repository
func (s *cfnControllerScenario) deleteFilesFromGitRepository(dir string, repo *git.Repository, filePaths ...string) error {
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	for _, filePath := range filePaths {
		if filePath != "" {
			if err = os.Remove(filePath); err != nil {
				return err
			}
		}
	}
	if _, err = w.Commit("Delete integ test files", &git.CommitOptions{All: true}); err != nil {
		return err
	}
	if err = repo.Push(&git.PushOptions{RemoteName: "origin", Auth: s.gitCredentials}); err != nil {
		return err
	}
	return nil
}

func (s *cfnControllerScenario) cleanup() error {
	if s.cleanedUp {
		return nil
	}

	if err := s.cmdRunner.run("kubectl", "delete", "--ignore-not-found=true", "-f", s.fluxConfigTemplateRepoConfigFile); err != nil {
		return err
	}

	s.deleteFilesFromGitRepository(s.fluxConfigRepoDir, s.fluxConfigRepo, s.fluxConfigTemplateRepoConfigFile)

	if s.fluxConfigRepoDir != "" {
		os.RemoveAll(s.fluxConfigRepoDir)
	}
	if s.cfnTemplateRepoDir != "" {
		os.RemoveAll(s.cfnTemplateRepoDir)
	}

	s.cleanedUp = true
	return nil
}

func (s *cfnControllerScenario) checkTemplateGitRepository(ctx context.Context) error {
	// Clone the Flux config repository locally, and add the template git repository configuration to the Flux config repo
	repo, dir, err := s.cloneGitRepository(ctx, FluxConfigRepoName)
	s.fluxConfigRepo = repo
	s.fluxConfigRepoDir = dir
	if err != nil {
		return err
	}

	// Add the template git repository configuration to the Flux config repo
	newFilePath, err := s.copyFileToGitRepository(dir, repo, "examples/my-flux-configuration/my-cloudformation-templates-repo.yaml")
	s.fluxConfigTemplateRepoConfigFile = newFilePath
	if err != nil {
		return err
	}

	// Validate that Flux can pull from the Flux config repo
	if err := s.cmdRunner.run("flux", "reconcile", "source", "git", "flux-system"); err != nil {
		return err
	}
	out, err := s.cmdRunner.getOutput("flux", "get", "sources", "git", "flux-system", "--status-selector", "ready=true", "--no-header")
	if err != nil {
		return err
	}
	if out == "" {
		s.cmdRunner.run("flux", "get", "sources", "git", "flux-system")
		return errors.New("Flux configuration repository could not be reconciled by Flux")
	}

	// Validate that Flux can pull from the CFN templates repo
	if err := s.cmdRunner.run("flux", "reconcile", "source", "git", "my-cfn-templates-repo"); err != nil {
		return err
	}
	out, err = s.cmdRunner.getOutput("flux", "get", "sources", "git", "my-cfn-templates-repo", "--status-selector", "ready=true", "--no-header")
	if err != nil {
		return err
	}
	if out == "" {
		s.cmdRunner.run("flux", "get", "sources", "git", "my-cfn-templates-repo")
		return errors.New("CloudFormation template file repository could not be reconciled by Flux")
	}

	// Clone the template git repository locally
	repo, dir, err = s.cloneGitRepository(ctx, CfnTemplateRepoName)
	s.cfnTemplateRepo = repo
	s.cfnTemplateRepoDir = dir
	if err != nil {
		return err
	}

	return nil
}

func (s *cfnControllerScenario) checkKubernetesCluster() error {
	if err := s.cmdRunner.run("kubectl", "version"); err != nil {
		return err
	}
	if err := s.cmdRunner.run("flux", "check"); err != nil {
		return err
	}
	return nil
}

func (s *cfnControllerScenario) applyCfnStackConfiguration(k8sConfig *godog.DocString) error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) deleteCfnStackObject() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) createCfnTemplateFile() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) updateCfnTemplateFile() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) createSecondCfnTemplateFile() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) reconcileGitRepository() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) realCfnStackShouldBeDeleted() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) realCfnStackShouldBeInState(expectedState string) error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) cfnStackObjectShouldBeDeleted() error {
	return godog.ErrPending
}

func (s *cfnControllerScenario) cfnStackObjectShouldHaveStatus(expectedStatus string) error {
	return godog.ErrPending
}
