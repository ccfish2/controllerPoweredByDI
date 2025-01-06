package endpointgc

import (
	"github.com/ccfish2/infra/pkg/metrics"
	"github.com/ccfish2/infra/pkg/metrics/metric"
)

const (
	LabelOutcome        = "Outcome"
	LabelOutcomeSuccess = "Success"
	LabelOutcomeFailed  = "Failed"
)

func NewMetrics() *Metrics {
	return &Metrics{
		EndpointGCObjects: metric.NewCounterVec(metric.CounterOpts{
			Namespace: metrics.DolphinOperatorNamespace,
			Name:      "metric name",
			Help:      "!!",
		}, []string{LabelOutcome}),
	}
}

type Metrics struct {
	EndpointGCObjects metric.Vec[metric.Counter]
}
