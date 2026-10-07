package predicates

import (
	"log/slog"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func GatewayOwnedByController(hasMatchingControllerFn func(object client.Object) bool, logger *slog.Logger) predicate.Predicate {
	evaluate := func(eventType string, obj client.Object) bool {
		started := time.Now()
		matched := hasMatchingControllerFn(obj)
		logger.Info("Gateway watch predicate evaluated",
			"eventType", eventType,
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
			"matched", matched,
			"duration", time.Since(started),
		)
		return matched
	}

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return evaluate("create", e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return evaluate("delete", e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Reconcile one last time when a Gateway moves away from this controller
			// so previously managed resources can be cleaned up.
			return evaluate("update-old", e.ObjectOld) || evaluate("update-new", e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return evaluate("generic", e.Object)
		},
	}
}
