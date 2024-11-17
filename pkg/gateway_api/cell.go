package gatewayapi

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"gateway-api",
	"Manages the Gateway API controllers",
)
