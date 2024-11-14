package metrics

import (
	"errors"
	"net/http"

	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/metrics/metric"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

type params struct {
	cell.In

	Logger     logrus.FieldLogger
	Lifecycle  cell.Lifecycle
	Shutdowner hive.Shutdowner

	Cfg          Config
	SharedConfig SharedConfig

	Metrics []metric.WithMetadata `group:"hive-metrics"`
}

type metricsManager struct {
	logger     logrus.FieldLogger
	shutdowner hive.Shutdowner

	server http.Server

	metrics []metric.WithMetadata
}

func (mm *metricsManager) Start(ctx cell.HookContext) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}))
	mm.server.Handler = mux

	go func() {
		mm.logger.WithField("address", mm.server.Addr).Info("Starting metrics server")
		if err := mm.server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			mm.logger.WithError(err).Error("Unable to start metrics server")
			mm.shutdowner.Shutdown()
		}
	}()

	return nil
}
