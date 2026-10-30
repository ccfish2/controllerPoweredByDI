package predicates

import (
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func GatewayClassOwnedByController(controllerName string) predicate.Predicate {
	hasMatchingControllerName := helpers.GatewayClassMatchesControllerName(controllerName)
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return hasMatchingControllerName(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return hasMatchingControllerName(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return hasMatchingControllerName(e.ObjectOld) || hasMatchingControllerName(e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return hasMatchingControllerName(e.Object)
		},
	}
}
