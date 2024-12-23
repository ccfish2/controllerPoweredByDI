package metrics

import (
	"errors"
	"net/http"

	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/metrics"
	"github.com/ccfish2/infra/pkg/metrics/metric"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	ctrlRuntimeMetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
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

func (mm *metricsManager) Stop(ctx cell.HookContext) error {
	if err := mm.server.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}

func registerMetricsManager(p params) {
	if !p.SharedConfig.EnableMetrics {
		return
	}
	mm := &metricsManager{
		logger:     p.Logger,
		server:     http.Server{Addr: p.Cfg.OperatorPrometheusServeAddr},
		shutdowner: p.Shutdowner,
		metrics:    p.Metrics,
	}
	if p.SharedConfig.EnableGatewayAPI {
		Registry = ctrlRuntimeMetrics.Registry
	} else {
		Registry = prometheus.NewPedanticRegistry()
	}
	Registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{Namespace: metrics.DolphinOperatorNamespace}))

	for _, metric := range mm.metrics {
		Registry.MustRegister(metric.(prometheus.Collector))
	}

	metrics.InitOperatorMetrics()
	Registry.MustRegister(metrics.ErrorsWarnings)
	metrics.FlushLoggingMetrics()

	p.Lifecycle.Append(mm)
}
