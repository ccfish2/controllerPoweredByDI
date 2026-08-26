package gateway_api

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/routechecker"
)

type GRPCRouteInput struct {
	Ctx       context.Context
	Logger    *logrus.Entry
	Client    client.Client
	Grants    *gatewayv1beta1.ReferenceGrantList
	GRPCRoute *gatewayv1.GRPCRoute

	gateways map[gatewayv1.ParentReference]*gatewayv1.Gateway
}

// GRPCRouteRule is used to implement the GenericRule interface for GRPCRoute
type GRPCRouteRule struct {
	Rule gatewayv1.GRPCRouteRule
}

type grpcrouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (g *GRPCRouteRule) GetBackendRefs() []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, b := range g.Rule.BackendRefs {
		refs = append(refs, b.BackendRef)
	}

	for _, f := range g.Rule.Filters {
		if f.Type == gatewayv1.GRPCRouteFilterRequestMirror {
			if f.RequestMirror == nil {
				continue
			}
			refs = append(refs, gatewayv1.BackendRef{
				BackendObjectReference: f.RequestMirror.BackendRef,
			})
		}
	}

	return refs
}

func (g *GRPCRouteInput) GetRules() []routechecker.GenericRule {
	var rules []routechecker.GenericRule
	for _, rule := range g.GRPCRoute.Spec.Rules {
		rules = append(rules, &GRPCRouteRule{rule})
	}
	return rules
}

func (g *GRPCRouteInput) GetNamespace() string {
	return g.GRPCRoute.GetNamespace()
}

func (g *GRPCRouteInput) GetClient() client.Client {
	return g.Client
}

func (g *GRPCRouteInput) GetContext() context.Context {
	return g.Ctx
}

func (g *GRPCRouteInput) GetGVK() schema.GroupVersionKind {
	return gatewayv1.SchemeGroupVersion.WithKind("GRPCRoute")
}

func (g *GRPCRouteInput) GetGrants() []gatewayv1beta1.ReferenceGrant {
	return g.Grants.Items
}

func (g *GRPCRouteInput) GetGateway(parent gatewayv1.ParentReference) (*gatewayv1.Gateway, error) {
	if g.gateways == nil {
		g.gateways = make(map[gatewayv1.ParentReference]*gatewayv1.Gateway)
	}

	if gw, exists := g.gateways[parent]; exists {
		return gw, nil
	}

	ns := helpers.NamespaceDerefOr(parent.Namespace, g.GetNamespace())
	gw := &gatewayv1.Gateway{}

	if err := g.Client.Get(g.Ctx, client.ObjectKey{Namespace: ns, Name: string(parent.Name)}, gw); err != nil {
		if !k8serrors.IsNotFound(err) {
			// if it is not just a not found error, we should return the error as something is bad
			return nil, fmt.Errorf("error while getting gateway: %w", err)
		}

		// Gateway does not exist skip further checks
		return nil, fmt.Errorf("gateway %q does not exist: %w", parent.Name, err)
	}

	g.gateways[parent] = gw
	return gw, nil
}

func (g *GRPCRouteInput) GetHostnames() []gatewayv1beta1.Hostname {
	return g.GRPCRoute.Spec.Hostnames
}

func (g *GRPCRouteInput) SetParentCondition(ref gatewayv1beta1.ParentReference, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.NewTime(time.Now())
	condition.ObservedGeneration = g.GRPCRoute.GetGeneration()

	g.mergeStatusConditions(ref, []metav1.Condition{
		condition,
	})
}

func (g *GRPCRouteInput) SetAllParentCondition(condition metav1.Condition) {
	// fill in the condition
	condition.LastTransitionTime = metav1.NewTime(time.Now())
	condition.ObservedGeneration = g.GRPCRoute.GetGeneration()

	for _, parent := range g.GRPCRoute.Spec.ParentRefs {
		g.mergeStatusConditions(parent, []metav1.Condition{
			condition,
		})
	}
}

func (g *GRPCRouteInput) Log() *logrus.Entry {
	return g.Logger
}

func (g *GRPCRouteInput) mergeStatusConditions(parentRef gatewayv1alpha2.ParentReference, updates []metav1.Condition) {
	index := -1
	for i, parent := range g.GRPCRoute.Status.RouteStatus.Parents {
		if reflect.DeepEqual(parent.ParentRef, parentRef) {
			index = i
			break
		}
	}
	if index != -1 {
		g.GRPCRoute.Status.RouteStatus.Parents[index].Conditions = merge(g.GRPCRoute.Status.RouteStatus.Parents[index].Conditions, updates...)
		return
	}
	g.GRPCRoute.Status.RouteStatus.Parents = append(g.GRPCRoute.Status.RouteStatus.Parents, gatewayv1alpha2.RouteParentStatus{
		ParentRef:      parentRef,
		ControllerName: controllerName,
		Conditions:     updates,
	})
}

func newGRPCRouteReconciler(mgr ctrl.Manager) *grpcrouteReconciler {
	return &grpcrouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *grpcrouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.GRPCRoute{},
		backendServiceIndex, getBackendServiceForGRPCRoute,
	); err != nil {
		return err
	}

	// Create field indexer for Gateway parents, this allows a faster lookup for event queueing
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.GRPCRoute{},
		gatewayIndex, getParentGatewayForGRPCRoute); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch for changes to GRPCRoute
		For(&gatewayv1.GRPCRoute{}).
		// Watch for changes to Backend services
		Watches(&corev1.Service{}, r.enqueueRequestForBackendService()).
		// Watch for changes to Reference Grants
		Watches(&gatewayv1beta1.ReferenceGrant{}, r.enqueueRequestForReferenceGrant()).
		// Watch for changes to Gateways and enqueue GRPCRoutes that reference them
		Watches(&gatewayv1beta1.Gateway{}, r.enqueueRequestForGateway(),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(hasMatchingController(context.Background(), mgr.GetClient(), controllerName)))).
		Complete(r)
}

func getParentGatewayForGRPCRoute(rawObj client.Object) []string {
	route, ok := rawObj.(*gatewayv1.GRPCRoute)
	if !ok {
		return nil
	}
	var gateways []string
	for _, parent := range route.Spec.ParentRefs {
		if !helpers.IsGateway(parent) {
			continue
		}
		gateways = append(gateways,
			types.NamespacedName{
				Namespace: helpers.NamespaceDerefOr(parent.Namespace, route.Namespace),
				Name:      string(parent.Name),
			}.String(),
		)
	}
	return gateways
}

func getBackendServiceForGRPCRoute(rawObj client.Object) []string {
	route, ok := rawObj.(*gatewayv1.GRPCRoute)
	if !ok {
		return nil
	}
	var backendServices []string
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.BackendRefs {
			if !helpers.IsService(backend.BackendObjectReference) {
				continue
			}
			backendServices = append(backendServices,
				types.NamespacedName{
					Namespace: helpers.NamespaceDerefOr(backend.Namespace, route.Namespace),
					Name:      string(backend.Name),
				}.String(),
			)
		}
	}
	return backendServices
}

// enqueueRequestForBackendService makes sure that GRPC Routes are reconciled
// if the backend services are updated.
func (r *grpcrouteReconciler) enqueueRequestForBackendService() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(backendServiceIndex))
}

// enqueueRequestForReferenceGrant makes sure that all GRPC Routes are reconciled
// if a ReferenceGrant changes
func (r *grpcrouteReconciler) enqueueRequestForReferenceGrant() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueAll())
}

func (r *grpcrouteReconciler) enqueueRequestForGateway() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(gatewayIndex))
}

func (r *grpcrouteReconciler) enqueueFromIndex(index string) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: grpcRoute,
			logfields.Resource:   client.ObjectKeyFromObject(o),
		})
		list := &gatewayv1.GRPCRouteList{}

		if err := r.Client.List(ctx, list, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(index, client.ObjectKeyFromObject(o).String()),
		}); err != nil {
			scopedLog.WithError(err).Error("Failed to get related GRPCRoutes")
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(list.Items))
		for _, item := range list.Items {
			route := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: route,
			})
			scopedLog.WithField(grpcRoute, route).Info("Enqueued GRPCRoute for resource")
		}
		return requests
	}
}

func (r *grpcrouteReconciler) enqueueAll() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: grpcRoute,
			logfields.Resource:   client.ObjectKeyFromObject(o),
		})
		list := &gatewayv1.GRPCRouteList{}

		if err := r.Client.List(ctx, list, &client.ListOptions{}); err != nil {
			scopedLog.WithError(err).Error("Failed to get GRPCRoutes")
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(list.Items))
		for _, item := range list.Items {
			route := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: route,
			})
			scopedLog.WithField(grpcRoute, route).Info("Enqueued GRPCRoute for resource")
		}
		return requests
	}
}
