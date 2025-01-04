package identitygc

import (
	"github.com/ccfish2/infra/pkg/metrics"
	"github.com/ccfish2/infra/pkg/metrics/metric"
)

const (
	LabelStatus  = "Status"
	LabelOutcome = "Outcome"
)

type Metrics struct {
	IdentityGCSize metric.Vec[metric.Gauge]
	IdentityGCRuns metric.Vec[metric.Gauge]
}

func NewMetrics() *Metrics {
	return &Metrics{
		IdentityGCSize: metric.NewGaugeVec(metric.GaugeOpts{
			Namespace: metrics.DolphinOperatorNamespace,
			Name:      "identity_gc_entrries",
			Help:      "this is the help message"}, []string{LabelStatus}),
		IdentityGCRuns: metric.NewGaugeVec(metric.GaugeOpts{
			Namespace: metrics.DolphinOperatorNamespace,
			Name:      "identity_gc_runs",
			Help:      "this is the help message",
		}, []string{LabelOutcome}),
	}
}
