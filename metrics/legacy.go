package metrics

import (
	"github.com/ccfish2/dolphin/api/v1/operator/models"
	"github.com/prometheus/client_golang/prometheus"
)

var Registry RegisterGatherer

type RegisterGatherer interface {
	prometheus.Registerer
	prometheus.Gatherer
}

func DumpMetrics() ([]*models.Metric, error) {
	// result is an array of models.Metric
	// if local variable Registry is nil, nothing to return
	// invoke Registry.Gather which collects all metrics
	// if error, return nothing
	// iterate all the metrics
	//	read its name and type
	//   iterate each Metric
	//		compose models.Metric using: metricName, value (calcualted based on metrictype, counter, gauge, histogram and untyped), labels(read from metric.labels)
	return nil, nil
}
