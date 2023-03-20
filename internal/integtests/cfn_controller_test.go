//go:build integration

package integtests

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

var opts = godog.Options{Output: colors.Colored(os.Stdout)}

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &opts)
}

func TestFeatures(t *testing.T) {
	o := opts
	o.TestingT = t

	status := godog.TestSuite{
		Name:                 "flux",
		Options:              &o,
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
	}.Run()

	if status == 2 {
		t.SkipNow()
	}

	if status != 0 {
		t.Fatalf("zero status code expected, %d received", status)
	}
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	ctx.BeforeSuite(func() {
		// TODO fill in
	})
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		// TODO fill in
		return ctx, nil
	})

	ctx.Step(`^I have a git repository for my CloudFormation template files registered with Flux$`, setupTemplateGitRepository)
	ctx.Step(`^I have a Kubernetes cluster with the CloudFormation controller for Flux installed$`, setupKubernetesCluster)
	ctx.Step(`^I apply the following CloudFormationStack configuration to my Kubernetes cluster$`, applyCfnStackConfiguration)
	ctx.Step(`^I mark the CloudFormationStack for deletion$`, deleteCfnStackObject)
	ctx.Step(`^I push a valid CloudFormation template to my git repository$`, createCfnTemplateFile)
	ctx.Step(`^I push an update for my CloudFormation template to my git repository$`, updateCfnTemplateFile)
	ctx.Step(`^I push another valid CloudFormation template to my git repository$`, createSecondCfnTemplateFile)
	ctx.Step(`^I trigger Flux to reconcile my git repository$`, reconcileGitRepository)
	ctx.Step(`^the CloudFormation stack in my AWS account should be deleted$`, realCfnStackShouldBeDeleted)
	ctx.Step(`^the CloudFormation stack in my AWS account should be in "([^"]*)" state$`, realCfnStackShouldBeInState)
	ctx.Step(`^the CloudFormationStack should eventually be deleted$`, cfnStackObjectShouldBeDeleted)
	ctx.Step(`^the CloudFormationStack\'s Ready condition should eventually have "([^"]*)" status$`, cfnStackObjectShouldHaveStatus)
}

func setupTemplateGitRepository() error {
	return godog.ErrPending
}

func setupKubernetesCluster() error {
	return godog.ErrPending
}

func applyCfnStackConfiguration(arg1 *godog.DocString) error {
	return godog.ErrPending
}

func deleteCfnStackObject() error {
	return godog.ErrPending
}

func createCfnTemplateFile() error {
	return godog.ErrPending
}

func updateCfnTemplateFile() error {
	return godog.ErrPending
}

func createSecondCfnTemplateFile() error {
	return godog.ErrPending
}

func reconcileGitRepository() error {
	return godog.ErrPending
}

func realCfnStackShouldBeDeleted() error {
	return godog.ErrPending
}

func realCfnStackShouldBeInState(arg1 string) error {
	return godog.ErrPending
}

func cfnStackObjectShouldBeDeleted() error {
	return godog.ErrPending
}

func cfnStackObjectShouldHaveStatus(arg1 string) error {
	return godog.ErrPending
}
