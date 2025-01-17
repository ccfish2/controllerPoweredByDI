package api

import (
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/metrics"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/go-openapi/runtime/middleware"

	opmetrics "github.com/ccfish2/controllerPoweredByDI/metrics"
)

// MetricsHandlerCell
// id, title
// the cell is metricsHandler
var MetricsHandlerCell = cell.Module(
	"operator metrics",
	"Operator Metrics Http Handler",
	cell.Provide(newMetricsHandler),
)

type metricsHandler struct{}

// Handle implements metrics.GetMetricsHandler.
func (m *metricsHandler) Handle(params metrics.GetMetricsParams) middleware.Responder {
	ret, err := opmetrics.DumpMetrics()
	if err != nil {
		return metrics.NewGetMetricsFailed()
	}
	return metrics.NewGetMetricsOK().WithPayload(ret)
}

func newMetricsHandler() metrics.GetMetricsHandler {
	return &metricsHandler{}
}
