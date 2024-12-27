package identitygc

import (
	"time"

	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

type SharedConfig struct {
	IdentityAllocationMode string
	K8sNamespace           string
}

var Cell = cell.Module(
	"identity-gc",
	"K8s Identity garbage collector",
	cell.Config(defaultConfig),
	cell.Invoke(registerGC),
	cell.Metric(NewMetrics),
)

type Config struct {
	Interval         time.Duration `mapstructure:"interval"`
	HeartbeatTimeout time.Duration `mapstructure:"heartbeat_timeout"`

	RateInterval time.Duration `mapstructure:"rate_interval"`
	RateLimit    int64         `mapstructure:"rate_limit"`
}

var defaultConfig = Config{}

func (def Config) Flags(flag *pflag.FlagSet) {

}
