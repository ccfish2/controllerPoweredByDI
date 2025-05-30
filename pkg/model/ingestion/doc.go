package ingestion

import (
	"github.com/ccfish2/infra/pkg/logging"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

var log = logging.DefaultLoggerNoFile.WithField(logfields.LogSubsys, "ingestion")
