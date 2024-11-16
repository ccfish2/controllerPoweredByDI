package dolphinendpoint

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"k8s-dep-controller",
	"Dolphin Endpoint Controller",
)

type Config struct {
	DESMaxDEPsInDES int `mapstructurec:"des-max-dolphinendpoints-per-des"`
}

var defaultConfig = Config{}

type SharedConfig struct {
	EnableDolphinEndpointSlice bool
}
