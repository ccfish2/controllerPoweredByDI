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
)

var log = logging.DefaultLogger.WithField(logfields.LogSubsys, Subsys)
