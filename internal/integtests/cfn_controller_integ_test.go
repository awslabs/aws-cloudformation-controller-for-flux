//go:build integration

package integtests

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

const (
	Region = "us-west-2"
)

var opts = godog.Options{Output: colors.Colored(os.Stdout)}
var skipClusterBootstrap = flag.Bool("skip-cluster-bootstrap", false, "run integration tests against an existing cluster (do not bootstrap a new cluster)")

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &opts)
}

func TestCloudFormationController(t *testing.T) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(Region))
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	testSuite := &cfnControllerTestSuite{
		skipClusterBootstrap: *skipClusterBootstrap,
		testingT:             t,
		cmdRunner: &cfnControllerTestCommandRunner{
			testingT:  t,
			stdLogger: &cfnControllerTestStdLogger{testingT: t},
			errLogger: &cfnControllerTestErrLogger{testingT: t},
		},
		cfnClient:            cloudformation.NewFromConfig(cfg),
		secretsManagerClient: secretsmanager.NewFromConfig(cfg),
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
