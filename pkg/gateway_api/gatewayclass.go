package gateway_api

import (
	"log/slog"

	watchhandlers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/watch-handlers"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type gatewayClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	controllerName string
	logger         *slog.Logger
}

func newGatewayClassReconciler(mgr ctrl.Manager, logger *slog.Logger, controllerName string) *gatewayClassReconciler {
	return &gatewayClassReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		controllerName: controllerName,
		logger:         logger,
	}
}

func (r *gatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.logger.Info("Registering GatewayClass controller",
		slog.String("controllerName", r.controllerName),
		slog.String("watchKind", "GatewayClass"),
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{},
			builder.WithPredicates(gatewayClassDebugPredicate(r.controllerName, r.logger))).
		Watches(&dolphinv2alpha1.DolphinGatewayClassConfig{}, watchhandlers.EnqueueRequestForCiliumGatewayClassConfig(r.Client, r.logger)).
		Complete(r)
}

func gatewayClassDebugPredicate(controllerName string, logger *slog.Logger) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			logger.Info("GatewayClass create event received", "name", e.Object.GetName(), "controllerName", controllerName)
			return matchesControllerName(controllerName)(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			logger.Info("GatewayClass delete event received", "name", e.Object.GetName(), "controllerName", controllerName)
			return matchesControllerName(controllerName)(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			logger.Info("GatewayClass update event received",
				"oldName", e.ObjectOld.GetName(),
				"newName", e.ObjectNew.GetName(),
				"controllerName", controllerName,
			)
			return matchesControllerName(controllerName)(e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			logger.Info("GatewayClass generic event received", "name", e.Object.GetName(), "controllerName", controllerName)
			return matchesControllerName(controllerName)(e.Object)
		},
	}
}

func matchesControllerName(controllerName string) func(object client.Object) bool {
	return func(obj client.Object) bool {
		if obj == nil {
			return false
		}

		gwc, ok := obj.(*gatewayv1.GatewayClass)
		if !ok || gwc == nil {
			return false
		}

		match := string(gwc.Spec.ControllerName) == controllerName
		log.Info("GatewayClass controller match check",
			"name", gwc.Name,
			"controllerName", controllerName,
			"actualControllerName", string(gwc.Spec.ControllerName),
			"match", match,
		)
		return match
	}
}
