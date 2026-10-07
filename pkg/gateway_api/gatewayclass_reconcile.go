package gateway_api

import (
	"context"
	"time"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	started := time.Now()
	r.logger.Info("GatewayClass reconcile started", "name", req.Name)
	defer func() {
		r.logger.Info("GatewayClass reconcile finished", "name", req.Name, "duration", time.Since(started))
	}()
	r.logger.Info("GatewayClass worker entered Reconcile", "name", req.Name)

	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info(" GatewayClass reconcile dequeued from queue ",
		"requestedName", req.Name,
		"namespacedName", req.NamespacedName,
	)
	scopedLog.Info("Reconciling GatewayClass", " requestedName ", req.Name)
	r.logger.Info("GatewayClass reconcile fetching object", "name", req.Name)
	origin := &gatewayv1.GatewayClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, origin); err != nil {
		if k8serrors.IsNotFound(err) {
			scopedLog.Info("GatewayClass no longer exists; skipping reconcile")
			return controllerruntime.Success()
		}
		scopedLog.Error("Failed to get GatewayClass during reconcile")
		return controllerruntime.Fail(err)
	}
	r.logger.Info("GatewayClass reconcile fetched object", "name", req.Name)

	if origin.GetDeletionTimestamp() != nil {
		scopedLog.Info("GatewayClass is being deleted; skipping reconcile")
		return controllerruntime.Success()
	}

	actualController := string(origin.Spec.ControllerName)
	matched := matchesControllerName(controllerName, nil, origin)
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
	r.logger.Info("GatewayClass reconcile patching status", "name", req.Name)
	if err := r.ensureStatus(ctx, gwc, origin); err != nil {
		scopedLog.Error("Failed to update GatewayClass status ", err.Error())
		return controllerruntime.Fail(err)
	}
	r.logger.Info("GatewayClass reconcile status patched", "name", req.Name)

	scopedLog.Info(" Successfully reconciled GatewayClass ")
	return controllerruntime.Success()
}

func (r *gatewayClassReconciler) ensureStatus(ctx context.Context, gwc *gatewayv1.GatewayClass, original *gatewayv1.GatewayClass) error {
	return r.Client.Status().Patch(ctx, gwc, client.MergeFrom(original))
}
