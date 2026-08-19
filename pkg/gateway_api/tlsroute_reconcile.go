package gateway_api

import (
	"context"
	"fmt"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/routechecker"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func (t *tlsrouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: "tlsRoute",
		logfields.Resource:   req.NamespacedName,
	})
	scopedLog.Info("Reconciling TLSRoute PassThrough")

	// Fetch the TLSRoute instance
	original := &gatewayv1alpha2.TLSRoute{}
	if err := t.Client.Get(ctx, req.NamespacedName, original); err != nil {
		if k8serrors.IsNotFound(err) {
			return controllerruntime.Success()
		}
		scopedLog.WithError(err).Error("Unable to fetch TLSRoute")
		return controllerruntime.Fail(err)
	}

	// Controller classic function: Ignore deleted TLSRoute, this can happen when foregroundDeletion is enabled
	if original.GetDeletionTimestamp() != nil {
		return controllerruntime.Success()
	}

	tr := original.DeepCopy()

	// check if this cert is allowed to be used by this gateway
	grants := &gatewayv1beta1.ReferenceGrantList{}
	if err := t.Client.List(ctx, grants); err != nil {
		return t.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to retrieve reference grants: %w", err), original, tr)
	}

	// input for the validators
	i := &routechecker.TLSRouteInput{
		Ctx:      ctx,
		Logger:   scopedLog.WithField(logfields.Resource, tr),
		Client:   t.Client,
		Grants:   grants,
		TLSRoute: tr,
	}

	// gateway validators
	for _, parent := range tr.Spec.ParentRefs {

		// set acceptance to okay, this wil be overwritten in checks if needed
		i.SetParentCondition(parent, metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionTrue,
			Reason:  string(gatewayv1.RouteReasonAccepted),
			Message: "Accepted TLSRoute",
		})

		// set status to okay, this wil be overwritten in checks if needed
		i.SetAllParentCondition(metav1.Condition{
			Type:    string(gatewayv1.RouteConditionResolvedRefs),
			Status:  metav1.ConditionTrue,
			Reason:  string(gatewayv1.RouteReasonResolvedRefs),
			Message: "Service reference is valid",
		})

		// run the actual validators
		for _, fn := range []routechecker.CheckGatewayFunc{
			routechecker.CheckGatewayAllowedForNamespace,
			routechecker.CheckGatewayRouteKindAllowed,
			routechecker.CheckGatewayMatchingPorts,
			routechecker.CheckGatewayMatchingHostnames,
			routechecker.CheckGatewayMatchingSection,
		} {
			continueCheck, err := fn(i, parent)
			if err != nil {
				return t.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to apply route check: %w", err), original, tr)
			}

			if !continueCheck {
				break
			}
		}
	}

	// backend validators

	for _, fn := range []routechecker.CheckRuleFunc{
		routechecker.CheckAgainstCrossNamespaceBackendReferences,
		routechecker.CheckBackendIsService,
		routechecker.CheckBackendIsExistingService,
	} {
		continueCheck, err := fn(i)
		if err != nil {
			return t.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to apply Gateway check: %w", err), original, tr)
		}

		if !continueCheck {
			break
		}
	}

	if err := t.updateStatus(ctx, original, tr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update TLSRoute status: %w", err)
	}

	scopedLog.Info("Successfully reconciled TLSRoute")
	return controllerruntime.Success()
}

func (t *tlsrouteReconciler) updateStatus(ctx context.Context, original *gatewayv1alpha2.TLSRoute, new *gatewayv1alpha2.TLSRoute) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	opts := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	if cmp.Equal(oldStatus, newStatus, opts) {
		return nil
	}
	return t.Client.Status().Update(ctx, new)
}

func (t *tlsrouteReconciler) handleReconcileErrorWithStatus(ctx context.Context, reconcileErr error, original *gatewayv1alpha2.TLSRoute, modified *gatewayv1alpha2.TLSRoute) (ctrl.Result, error) {
	if err := t.updateStatus(ctx, original, modified); err != nil {
		return controllerruntime.Fail(fmt.Errorf("failed to update TLSRoute status while handling the reconcile error %w: %w", reconcileErr, err))
	}

	return controllerruntime.Fail(reconcileErr)
}
