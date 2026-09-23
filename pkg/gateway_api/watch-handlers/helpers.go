package watchhandlers

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/indexers"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// updateReconcileRequestsForParentRefs mutates the passed reconcile.Request set
// to add all referenced Gateways, both via Gateway and via ListenerSet
func updateReconcileRequestsForParentRefs(ctx context.Context, c client.Client, parentRefs []gatewayv1.ParentReference, ns string, allGatewaysSet map[string]struct{}, rrSet map[reconcile.Request]struct{}) {
	for _, parent := range parentRefs {
		if helpers.IsGateway(parent) {
			parentFullName := types.NamespacedName{
				Name:      string(parent.Name),
				Namespace: helpers.NamespaceDerefOr(parent.Namespace, ns),
			}
			if _, found := allGatewaysSet[parentFullName.String()]; found {
				rrSet[reconcile.Request{NamespacedName: parentFullName}] = struct{}{}
			}
			continue
		}

		if helpers.IsListenerSet(parent) {
			gwNN := helpers.ResolveListenerSetToGateway(ctx, c, string(parent.Name), helpers.NamespaceDerefOr(parent.Namespace, ns))
			if gwNN != nil {
				if _, found := allGatewaysSet[gwNN.String()]; found {
					rrSet[reconcile.Request{NamespacedName: *gwNN}] = struct{}{}
				}
			}
		}
	}
}

func getAllGatewaysSetForController(ctx context.Context, c client.Client, controllerName string) (map[string]struct{}, error) {
	// Fetch all Gateways for the target controller using the
	// indexers.ImplementationGatewayIndex.
	gwList := &gatewayv1.GatewayList{}
	if err := c.List(ctx, gwList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(indexers.ImplementationGatewayIndex, controllerName),
	}); err != nil {
		return nil, err
	}
	// Build a set of all matching Gateway full names.
	// This makes sure we only add a reconcile.Request once for each Gateway.
	allGatewaysSet := make(map[string]struct{})

	for _, gw := range gwList.Items {
		gwFullName := types.NamespacedName{
			Name:      gw.GetName(),
			Namespace: gw.GetNamespace(),
		}
		allGatewaysSet[gwFullName.String()] = struct{}{}
	}

	return allGatewaysSet, nil
}
