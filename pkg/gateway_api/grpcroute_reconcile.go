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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// The main purpose of the reconcile is that transform current GRPC resource state closer to desired state
// mainly validate the GRPC route resources are valid and accepted. With so, we add this grpc route resource into parent gateway
// for further processing
func (g *grpcrouteReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// update grpc status
	scopedLog := log.WithContext(ctx).WithField("reconciling grpcroute", logrus.Fields{
		logfields.Controller: grpcRoute,
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.WithFields(logrus.Fields{
		"routeVersion": gatewayv1.GroupVersion.String(),
		"routeName":    req.NamespacedName.String(),
	}).Info("GRPCRoute reconcile started")
	scopedLog.Info("Reconcile GRPCRoute")

	// Fetch GRPCRoute instance
	original := &gatewayv1.GRPCRoute{}
	scopedLog.WithFields(logrus.Fields{
		"parentRefs": original.Spec.ParentRefs,
		"hostnames":  original.Spec.Hostnames,
		"rules":      len(original.Spec.Rules),
	}).Info("Loaded GRPCRoute")
	if err := g.Client.Get(ctx, req.NamespacedName, original); err != nil {
		if k8serrors.IsNotFound(err) {
			return controllerruntime.Success()
		}
		scopedLog.WithError(err).Error("Unable to fetch GRPCRoute")
		return controllerruntime.Fail(err)
	}
	// and ignore deletion status resource
	if original.GetDeletionTimestamp() != nil {
		return controllerruntime.Success()
	}

	// make a deepcopy of the instance, which is the cluster status
	gr := original.DeepCopy()

	// check if the backend allow
	grants := &gatewayv1beta1.ReferenceGrantList{}
	if err := g.Client.List(ctx, grants); err != nil {
		return g.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to retrieve reference grants: %w", err), original, gr)
	}

	// build one input for the validators
	i := &GRPCRouteInput{
		Ctx:       ctx,
		Logger:    scopedLog.WithField(logfields.Resource, gr),
		Client:    g.Client,
		Grants:    grants,
		GRPCRoute: gr, // deepcopy of requested instance
	}

	// gateway validators
	for _, parent := range gr.Spec.ParentRefs {
		// update parent condition
		// set acceptance to okay, this wil be overwritten in checks if needed
		i.SetParentCondition(parent, metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionTrue,
			Reason:  string(gatewayv1.RouteReasonAccepted),
			Message: "Accepted GRPCRoute",
		})
		// set all parentcondition
		// set status to okay, this wil be overwritten in checks if needed
		i.SetAllParentCondition(metav1.Condition{
			Type:    string(gatewayv1beta1.RouteConditionResolvedRefs),
			Status:  metav1.ConditionTrue,
			Reason:  string(gatewayv1beta1.RouteReasonResolvedRefs),
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

			scopedLog.WithFields(logrus.Fields{
				"validator": fmt.Sprintf("%T", fn),
				"continue":  continueCheck,
				"error":     err,
				"parent":    parent.Name,
			}).Info("GRPCRoute gateway validator completed")
			if err != nil {
				return g.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to apply Gateway check: %w", err), original, gr)
			}

			if !continueCheck {
				break
			}
		}
	}

	for _, fn := range []routechecker.CheckRuleFunc{
		routechecker.CheckAgainstCrossNamespaceBackendReferences,
		routechecker.CheckBackendIsService,
		routechecker.CheckBackendIsExistingService,
	} {
		if continueCheck, err := fn(i); err != nil || !continueCheck {
			return g.handleReconcileErrorWithStatus(ctx, fmt.Errorf("failed to apply Backend check: %w", err), original, gr)
		}
	}

	scopedLog.WithField("parents", gr.Status.Parents).
		Info("Updating GRPCRoute status")
	if err := g.updateStatus(ctx, original, gr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update GRPCRoute status: %w", err)
	}

	scopedLog.Info("Successfully reconciled GRPCRoute")
	return controllerruntime.Success()
}

// as the function name said, update resource status
func (g *grpcrouteReconciler) updateStatus(ctx context.Context, original *gatewayv1.GRPCRoute, new *gatewayv1.GRPCRoute) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	opts := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	if cmp.Equal(oldStatus, newStatus, opts) {
		return nil
	}
	return g.Client.Status().Update(ctx, new)
}

// we update the status when facing one error handle reconciliation
func (g *grpcrouteReconciler) handleReconcileErrorWithStatus(ctx context.Context, reconcileErr error, original *gatewayv1.GRPCRoute, modified *gatewayv1.GRPCRoute) (ctrl.Result, error) {
	if err := g.updateStatus(ctx, original, modified); err != nil {
		return controllerruntime.Fail(fmt.Errorf("failed to update GRPCRoute status while handling the reconcile error %w: %w", reconcileErr, err))
	}

	return controllerruntime.Fail(reconcileErr)
}
