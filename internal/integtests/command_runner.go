//go:build integration

package integtests

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type cfnControllerTestCommandRunner struct {
	testingT  *testing.T
	stdLogger *cfnControllerTestStdLogger
	errLogger *cfnControllerTestErrLogger
}

type cfnControllerTestStdLogger struct {
	testingT *testing.T
}

// Use custom writers so that we can pipe command output to the testing framework's logger
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
		t.testingT.Error(output)
		if ee, ok := err.(*exec.ExitError); ok {
			t.testingT.Error(string(ee.Stderr))
		}
		t.testingT.Error(err)
		return err
	}
	return nil
}

func (t *cfnControllerTestCommandRunner) runWithStdIn(stdinContent string, command string, arg ...string) error {
	// current working directory is expected to be <root>/internal/integtests
	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.testingT.Error(err)
		return err
	}

	cmd := exec.Command(command, arg...)
	cmd.Dir = rootDir
	cmd.Stdin = strings.NewReader(stdinContent)
	output, err := cmd.Output()
	if err != nil {
		t.testingT.Error(fmt.Sprintf("Command failed: %s %s", command, strings.Join(arg, " ")))
		t.testingT.Error(stdinContent)
		t.testingT.Error(output)
		if ee, ok := err.(*exec.ExitError); ok {
			t.testingT.Error(string(ee.Stderr))
		}
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
		t.testingT.Error(output)
		if ee, ok := err.(*exec.ExitError); ok {
			t.testingT.Error(string(ee.Stderr))
		}
		t.testingT.Error(err)
		return "", err
	}

	return string(output), nil
}
