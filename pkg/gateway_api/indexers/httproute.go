package indexers

import (
	"log/slog"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// GenerateIndexerHTTPRouteByBackendService makes a client.IndexerFunc that takes a single HTTPRoute and
// returns all referenced backend service full names (`namespace/name`) to add to the relevant index.
func GenerateIndexerHTTPRouteByBackendService(c client.Client, logger *slog.Logger) client.IndexerFunc {
	return func(rawObj client.Object) []string {
		route, ok := rawObj.(*gatewayv1.HTTPRoute)
		if !ok {
			return nil
		}
		var backendServices []string

		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				namespace := helpers.NamespaceDerefOr(backend.Namespace, route.Namespace)
				backendServiceName, err := helpers.GetBackendServiceName(c, namespace, backend.BackendObjectReference)
				if err != nil {
					logger.Error("Failed to get backend service name",
						logfields.LogSubsys, logfields.HTTPRoute,
						logfields.HTTPRoute, client.ObjectKeyFromObject(rawObj),
						logfields.Error, err)
					continue
				}
				backendServices = append(backendServices,
					types.NamespacedName{
						Namespace: helpers.NamespaceDerefOr(backend.Namespace, route.Namespace),
						Name:      backendServiceName,
					}.String(),
				)
			}
		}
		return backendServices
	}
}
