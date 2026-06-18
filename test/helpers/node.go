package helpers

import (
	"os"

	ginkgoext "github.com/ccfish2/controllerPoweredByDI/test/ginkgo-ext"
)

var (
	SSHMetaLogs = ginkgoext.NewWriter(os.Stdout)
)
