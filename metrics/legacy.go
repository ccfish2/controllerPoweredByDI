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
	return nil, nil
}
