package k8s

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s"
	corev1 "k8s.io/api/core/v1"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/k8s/resource"
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

const (
	DolphinEndpointIndexIdentity = "identity"
)

type Resources struct {
	cell.In

	Services              resource.Resource[*corev1.Service]
	Endpoints             resource.Resource[*corev1.Endpoints]
	DolphinEndpoints      resource.Resource[*dolphinv1.DolphinEndpoint]
	DolphinEndpointSlices resource.Resource[*dolphinv1.DolphinEndpointSlice]
	Pods                  resource.Resource[*corev1.Pod]
}
