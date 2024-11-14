package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var Registry RegisterGatherer

type RegisterGatherer interface {
	prometheus.Registerer
	prometheus.Gatherer
}
