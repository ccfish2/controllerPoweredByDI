package gateway_api

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"gateway-api",
	"Managed the Gateway API controllers",
)
