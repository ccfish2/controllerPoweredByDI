package gateway_api

import (
	"context"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: "gatewayclass",
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info("Reconciling GatewayClass", "requestedName", req.Name)
	origin := &gatewayv1.GatewayClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, origin); err != nil {
		if k8serrors.IsNotFound(err) {
			scopedLog.Info("GatewayClass no longer exists; skipping reconcile")
			return controllerruntime.Success()
		}
		scopedLog.WithError(err).Error("Failed to get GatewayClass during reconcile")
		return controllerruntime.Fail(err)
	}

	if origin.GetDeletionTimestamp() != nil {
		scopedLog.Info("GatewayClass is being deleted; skipping reconcile")
		return controllerruntime.Success()
	}

	actualController := string(origin.Spec.ControllerName)
	matched := matchesControllerName(controllerName)(origin)
	scopedLog.WithField("actualControllerName", actualController).WithField("expectedControllerName", controllerName).WithField("matched", matched).Info("GatewayClass controller match result")
	if !matched {
		scopedLog.WithField("controllerName", actualController).Debug("Ignoring GatewayClass for a different controller")
		return controllerruntime.Success()
	}

	gwc := origin.DeepCopy()
	setGatewayClassAccepted(gwc, true)
	setGatewayClassSupportedFeatures(gwc)
	if err := r.ensureStatus(ctx, gwc, origin); err != nil {
		scopedLog.Errorf("Failed to update GatewayClass status %v", err)
		return controllerruntime.Fail(err)
	}

	scopedLog.Info("Successfully reconciled GatewayClass")
	return controllerruntime.Success()
}

func (r *gatewayClassReconciler) ensureStatus(ctx context.Context, gwc *gatewayv1.GatewayClass, original *gatewayv1.GatewayClass) error {
	return r.Client.Status().Patch(ctx, gwc, client.MergeFrom(original))
}
