package k8s

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s"
)

var (
	ResourcesCell = cell.Module(
		"k8s-resources",
		"Operator Kubernetes resources",

		cell.Config(k8s.DefaultConfig),
		cell.Provide(
			DolphinEndpointResource,
		),
	)
)
