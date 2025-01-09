package api

import (
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/metrics"
	"github.com/go-openapi/runtime/middleware"

	opmetrics "github.com/ccfish2/controllerPoweredByDI/metrics"
)

type metricsHandler struct{}

// Handle implements metrics.GetMetricsHandler.
func (m *metricsHandler) Handle(params metrics.GetMetricsParams) middleware.Responder {
	opmetrics.DumpMetrics()
	return nil
}

func newMetricsHandler() metrics.GetMetricsHandler {
	return &metricsHandler{}
}
