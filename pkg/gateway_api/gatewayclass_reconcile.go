package gateway_api

import (
	"context"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := r.logger.With(
		logfields.Resource, req.NamespacedName,
	)

	scopedLog.Info("GatewayClass reconcile dequeued from queue",
		"requestedName", req.Name,
		"namespacedName", req.NamespacedName,
	)
	scopedLog.Info("Reconciling GatewayClass", "requestedName", req.Name)
	origin := &gatewayv1.GatewayClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, origin); err != nil {
		if k8serrors.IsNotFound(err) {
			scopedLog.Info("GatewayClass no longer exists; skipping reconcile")
			return controllerruntime.Success()
		}
		scopedLog.Error("Failed to get GatewayClass during reconcile")
		return controllerruntime.Fail(err)
	}

	if origin.GetDeletionTimestamp() != nil {
		scopedLog.Info("GatewayClass is being deleted; skipping reconcile")
		return controllerruntime.Success()
	}

	actualController := string(origin.Spec.ControllerName)
	matched := matchesControllerName(controllerName, scopedLog, origin)
	scopedLog.Info("GatewayClass reconcile match result",
		"actualControllerName", actualController,
		"expectedControllerName", controllerName,
		"matched", matched,
	)
	if !matched {
		scopedLog.Info("Ignoring GatewayClass for a different controller",
			"actualControllerName", actualController,
			"expectedControllerName", controllerName,
		)
		return controllerruntime.Success()
	}

	gwc := origin.DeepCopy()
	setGatewayClassAccepted(gwc, true)
	setGatewayClassSupportedFeatures(gwc)
	if err := r.ensureStatus(ctx, gwc, origin); err != nil {
		scopedLog.Error("Failed to update GatewayClass status ", err.Error())
		return controllerruntime.Fail(err)
	}

	scopedLog.Info("Successfully reconciled GatewayClass")
	return controllerruntime.Success()
}

func (r *gatewayClassReconciler) ensureStatus(ctx context.Context, gwc *gatewayv1.GatewayClass, original *gatewayv1.GatewayClass) error {
	return r.Client.Status().Patch(ctx, gwc, client.MergeFrom(original))
}
