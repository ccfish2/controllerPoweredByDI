package gateway_api

import (
	"context"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: "gatewayclass",
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info("Reconciling GatewayClass")
	gwc := &gatewayv1.GatewayClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, gwc); err != nil {
		if k8serrors.IsNotFound(err) {
			return controllerruntime.Success()
		}
		return controllerruntime.Fail(err)
	}

	if gwc.GetDeletionTimestamp() != nil {
		return controllerruntime.Success()
	}

	setGatewayClassAccepted(gwc, true)

	// List of features supported by Dolphn.
	gwc.Status.SupportedFeatures = []gatewayv1.SupportedFeature{
		gatewayv1.SupportedFeature{Name: "Gateway"},
		gatewayv1.SupportedFeature{Name: "TLSRoute"},
		gatewayv1.SupportedFeature{Name: "HTTPRoute"},
		gatewayv1.SupportedFeature{Name: "ReferenceGrant"},
	}

	if err := r.Client.Status().Update(ctx, gwc); err != nil {
		scopedLog.WithError(err).Error("Failed to update GatewayClass status")
		return controllerruntime.Fail(err)
	}
	scopedLog.Info("Successfully reconciled GatewayClass")
	return controllerruntime.Success()
}
