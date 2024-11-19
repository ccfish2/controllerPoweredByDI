package gatewayapi

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	k8serros "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	//myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/gateway_api/model/ingestion"
)

func (r *gatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Println("Reconciling Gateway")

	// step 1: retrieve the gateway
	gw := &gatewayv1.Gateway{}
	err := r.Client.Get(ctx, req.NamespacedName, gw)
	if err != nil {
		if k8serros.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failure")
	}

	// ignore deleting gateway
	if gw.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	copy := gw.DeepCopy()

	// reconcile gateway
	gwc := &gatewayv1.GatewayClass{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: string(copy.Spec.GatewayClassName)}, gwc)
	if err != nil {
		fmt.Println("failed getting GatewayClass")
		if k8serros.IsNotFound(err) {
			setGatewayAccepted(copy, false, "GatewayClass does not exist")
			return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
		}
	}

	// handle HTTPRouteList, TLSRouteList, ServiceList
	httpRouteList := &gatewayv1.HTTPRouteList{}
	err = r.Client.List(ctx, httpRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tlsRouteList := &gatewayv1alpha2.TLSRouteList{}
	err = r.Client.List(ctx, tlsRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	servicesList := &corev1.ServiceList{}
	err = r.Client.List(ctx, servicesList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}
	httpListeners, tlsListeners := ingestion.GatewayAPI(ingestion.Input{
		GatewayClass: *gwc,
		Gateway:      *gw,
		HTTPRoutes:   r.filterHTTPRoutesByGateway(ctx, gw, httpRouteList.Items),
		TLSRoutes:    r.filterTLSRoutesByGateway(ctx, gw, tlsRouteList.Items),
		Services:     servicesList.Items,
	})
	err = r.setListenerStatus(ctx, gw, httpRouteList, tlsRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	// step 3: translate the listenersinto dolphin model
	_ = httpListeners
	_ = tlsListeners
	// step 4: update the status of the
	return reconcile.Result{}, nil
}

func (r *gatewayReconciler) setListenerStatus(ctx context.Context, gw *gatewayv1.Gateway, httpRoutes *gatewayv1.HTTPRouteList, tlsRoutes *gatewayv1alpha2.TLSRouteList) error {
	panic("steps")
}

func (r *gatewayReconciler) filterHTTPRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1.HTTPRoute) []gatewayv1.HTTPRoute {
	panic("stesp")
}

func (r *gatewayReconciler) filterTLSRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1alpha2.TLSRoute) []gatewayv1alpha2.TLSRoute {
	panic("steps")
}

func (r *gatewayReconciler) updateStatus(ctx context.Context, original, modified *gatewayv1.Gateway) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := modified.Status.DeepCopy()

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
		return nil
	}
	return r.Client.Status().Update(ctx, modified)
}
func (r *gatewayReconciler) handleReconcileErrorWithStatus(ctx context.Context, reconcileRR error, original, modified *gatewayv1.Gateway) (ctrl.Result, error) {
	err := r.updateStatus(ctx, original, modified)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("FAILED TO UPATE")
	}
	return ctrl.Result{}, reconcileRR
}
