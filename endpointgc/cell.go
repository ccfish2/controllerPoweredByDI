package endpointgc

import (
	"time"

	"github.com/ccfish2/infra/pkg/hive/cell"
)

var Cell = cell.Module(
	"k8s-endpoints-gc",
	"Dolphin endpoints garbage collector",
	cell.Invoke(registerGC),
)

type SharedConfig struct {
	Interval                  time.Duration
	DisableDolphinEndpointCRD bool
}
