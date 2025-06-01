package ingress

import (
	"github.com/ccfish2/infra/pkg/logging"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

const (
	Subsys = "Ingress"
)

var log = logging.DefaultLoggerNoFile.WithField(logfields.LogSubsys, Subsys)
