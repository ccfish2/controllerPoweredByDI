package controllerruntime

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"controller-runtime",
	"Manages the controller-runtime integration and its components",
)
