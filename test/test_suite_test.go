package test_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/gops/agent"

	ginkgoext "github.com/ccfish2/controllerPoweredByDI/test/ginkgo-ext"
	"github.com/ccfish2/controllerPoweredByDI/test/helpers"
	"github.com/ccfish2/infra/pkg/logging"
	. "github.com/onsi/ginkgo/v2"
)

var (
	log            = logging.DefaultLogger
	DefaultSetting = map[string]string{
		"k8s-version": "1.31",
	}
	commandsLogFile = "commands.log"
)

func init() {
	if err := agent.Listen(agent.Options{ShutdownCleanup: true}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start gops: %v\n", err)
		os.Exit(1)
	}
	for k, v := range DefaultSetting {
		os.Setenv(k, v)
	}
	os.RemoveAll(helpers.TestResultsPath)
}

func configLogsOutput() {
	log.Debug("Configuring test output")
}

func showCommands() {
	// TODO: Implement command display
	helpers.SSHMetaLogs = ginkgoext.NewWriter(os.Stdout)
}

func TestSuite(t *testing.T) {
	configLogsOutput()
	showCommands()
	RunSpecs(t, "Test Suite")
}

func goReportSetupStatus() chan bool {
	exit := make(chan bool)
	go func() {
		iter := 0
		done := false
		for {
			select {
			case ok := <-exit:
				if ok {
					fmt.Fprintf(os.Stdout, "Setup success\n")
				} else {
					fmt.Fprintf(os.Stdout, "Setup failed\n")
				}
				done = true
			default:
				fmt.Fprintf(os.Stdout, "Setup status: %d\n", iter%4)
			}
			if done {
				return
			}
			time.Sleep(250 * time.Millisecond)
			iter++
		}
	}()

	return exit
}

func reportCreateVMFailure(vm string, err error) {
	failmsg := fmt.Sprintf("Failed to create VM %s: %v", vm, err)
	log.Error(failmsg)
	Fail(failmsg)
}

var _ = BeforeSuite(func() {
	By("Starting tests")
})

var _ = AfterSuite(func() {
	scope, _ := helpers.GetScope()
	By(fmt.Sprintf("Cleaning up test scope: %s", scope))
})

var _ = Describe("Test Suite", func() {
	BeforeEach(func() {
		defer helpers.SSHMetaLogs.Reset()
	})
})

func getOrSetEnvVar(k, v string) {
	val := os.Getenv(k)
	if val == "" {
		os.Setenv(k, v)
	}
}
