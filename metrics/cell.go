package metrics

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

var Cell = cell.Module(
	"operator-metrics",
	"Operator Metrics",
	cell.Config(defaultConfig),
	cell.Invoke(registerMetricsManager),
)

const (
	OperatorPrometheusServeAddr = "operator-prometheus-serve-addr"
)

type Config struct {
	OperatorPrometheusServeAddr string
}

var defaultConfig = Config{
	OperatorPrometheusServeAddr: ":9963",
}

func (def Config) Flags(flags *pflag.FlagSet) {
	flags.String(OperatorPrometheusServeAddr, def.OperatorPrometheusServeAddr, "Address to serve Prometheus metrics")
}

type SharedConfig struct {
	EnableMetrics    bool
	EnableGatewayAPI bool
}
