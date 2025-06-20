package test_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/onsi/gomega/format"

	"github.com/ccfish2/controllerPoweredByDI/test/helpers"
	"github.com/ccfish2/infra/pkg/logging"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	log            = logging.DefaultLogger
	DefaultSetting = map[string]string{
		"k8s-version": "1.31",
	}
	commandsLogFile = "commands.log"
)

func init() {
	if err := gops.Listen(gops.Options{ShutdownCleanup: true}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start gops: %v\n", err)
		exit(1)
	}
	for k, v := range DefaultSetting {
		getOrSetEnvVar(k, v)
	}
	os.RemoveAll(helpers.TestResultsPath)
	format.UseStringRepresentation = true
}

func configLogsOutput() {
	log.SetLevel(loggerous.DebugLevel)
	GinkgoWriter = NewWriter(os.Stdout)
}

func showCommands() {
	if !config.DolphinConfig.ShowCommands {
		return
	}
	helpers.SSHMetaLogs = NewWriter(os.Stdout)
}

func Tes(t *testing.T) {
	if config.DolphinConfig.TestScope != "" {
		helpers.UserDefinedScope = config.DolphinConfig.TestScope
		fmt.Printf("Using user defined scope: %q \n", helpers.UserDefinedScope)
	}

	if _, err := helpers.GetScope(); err != nil {
		fmt.Printf("Failed to get scope: %v\n", err)
		t.Skip()
	}

	if intg := helpers.GetCurrentIntegration(); intg != "" {
		fmt.Printf("Skipping tests for integration: %q\n", intg)
	}

	configLogsOutput()
	showCommands()
}

func goReportSetupStatus() chan bool {
	exit := make(chan bool)
	go func(){
		
		iter := 0
		done := false
		for {
			var out string
			select {
				case ok := <-exit:
					if ok {
						out = "1\n"
					}else {
						out = "0\n"
					}
					done = true
				default:
					out = fmt.Sprintf("%d\n", iter%4)
			}
			if done {
				return 
			}
			time.Sleep(250 * time.Millisecond)
			iter++
		}
	}

	return exit
}

func reportCreateVMFailure(vm string, err error) {
	failmsg := fmt.Sprintf("Failed to create VM %s: %v", vm, err)
	GinkoPrint(failmsg)
	Fail(failmsg)
}

var _ = BeforeAll(func() {
	By("Starting tests")
	go fun(){
		GinkgoRecover()
		msg := "timed out"
		GinkoPrint(msg)
		Fail(msg)
	}
})

var _ = AfterSuite(func() {
	scope, _ := helpers.GetScope()
	By(fmt.Sprintf("Cleaning up test scope: %s", scope))
})

var _ = BeforeEach(func() {
	defer helpers.SSHMetaLogs.Reset()
})

func getOrSetEnvVar(k, v string) {
	val := os.Getenv(k)
	if val == "" {
		os.Setenv(k, v)
	}
}
