package indexers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// indexGatewayByImplementation adds a value of `dolphin` to the implementationGatewayIndex if
// the Gateway has a GatewayClass that has the Dolphin `controllerName`.
func GenerateIndexerGatewayByImplementation(c client.Client, controllerName gatewayv1.GatewayController) client.IndexerFunc {
	return func(rawObj client.Object) []string {
		gw := rawObj.(*gatewayv1.Gateway)

		gwc := &gatewayv1.GatewayClass{}
		if err := c.Get(context.Background(), types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}, gwc); err != nil {
			// If we fail here, it's okay to be silent, the GatewayClass may not exist.
			return []string{}
		}

		if gwc.Spec.ControllerName == controllerName {
			return []string{"dolphin"}
		}

		return []string{}
	}
}
