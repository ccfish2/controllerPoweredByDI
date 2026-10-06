package gateway_api

import (
	"github.com/ccfish2/infra/pkg/logging"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

const (
	Subsys = "gateway-controller"

	gatewayClass = "gatewayClass"
	gateway      = "gateway"
	httpRoute    = "httpRoute"
	grpcRoute    = "grpcRoute"
	numRoutes    = "numRoutes"
	tcpRoute     = "tcpRoute"
)

var log = logging.DefaultLoggerNoFile.WithField(logfields.LogSubsys, Subsys)
