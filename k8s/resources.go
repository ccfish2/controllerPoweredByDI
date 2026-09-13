package k8s

import (
	"github.com/ccfish2/infra/pkg/hive/cell"

	// dolphin
	"github.com/ccfish2/infra/pkg/k8s"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/k8s/resource"
)

/*
// ResourceCell provides a set of handles to Kubernetes resources used throughout the 
// operator. 
// Each of the resources share a client-go informer and backing store so we only have one
// watch API call for each resource kind and that we maintain only one copy of each object
*/
var (
	ResourcesCell = cell.Module(
		"k8s-resources",
		"Operator Kubernetes resources",

		cell.Config(k8s.DefaultConfig),
		cell.Provide(
			DolphinEndpointResource,
			DolphinEndpointSliceResource,
			k8s.DolphinIdentityResource,
			k8s.DolphinNodeResource,
		),
	)
)

const (
	DolphinEndpointIndexIdentity = "identity"
)

type Resources struct {
	cell.In

	DolphinEndpoints resource.Resource[*dolphinv1.DolphinEndpoint]
	// Services              resource.Resource[*corev1.Service]
	// Endpoints             resource.Resource[*corev1.Endpoints]
	DolphinEndpointSlices resource.Resource[*dolphinv1.DolphinEndpointSlice]
	Identities            resource.Resource[*dolphinv1.DolphinIdentity]
	//Pods                  resource.Resource[*corev1.Pod]
}
