package identitygc

import (
	"time"

	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

const (
	Interval         = "identitygc-interval"
	HeartbeatTimeout = "identitygc-heartbeat-timeout"
	RateInterval     = "identitygc-rate-interval"
	RateLimit        = "identitygc-rate-limit"
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
	Interval         time.Duration `mapstructure:"identitygc-interval"`
	HeartbeatTimeout time.Duration `mapstructure:"identitygc-heartbeat-timeout"`

	RateInterval time.Duration `mapstructure:"identitygc-rate-interval"`
	RateLimit    int64         `mapstructure:"identitygc-rate-limit"`
}

var defaultConfig = Config{
	Interval:         15 * time.Minute,
	HeartbeatTimeout: 2 * 15 * time.Minute,

	RateInterval: time.Minute,
	RateLimit:    2500,
}

func (def Config) Flags(flag *pflag.FlagSet) {
	flag.Duration(Interval, def.Interval, "GC interval")
	flag.Duration(HeartbeatTimeout, def.HeartbeatTimeout, "GC interval")
	flag.Duration(RateInterval, def.RateInterval, "Rate limit interval")
	flag.Int64(RateLimit, def.RateLimit, "Rate limit")
}
