package gateway_api

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	owningGatewayLabel = "io.dolphin.gateway/owning-gateway"
	lastTransitionTime = "LastTransitionTime"
)

type gatewayReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SecretNamespace    string
	IdleTimeoutSeconds int
	EnableIPv4         bool
	EnableIPv6         bool
}

func newGatewayReconciler(mgr ctrl.Manager, secretsNamespace string, idleTimeoutSeconds int, enableIpv4 bool, enableIpv6 bool) *gatewayReconciler {
	return &gatewayReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		SecretNamespace:    secretsNamespace,
		IdleTimeoutSeconds: idleTimeoutSeconds,
		EnableIPv4:         enableIpv4,
		EnableIPv6:         enableIpv6,
	}
}

// sets up the controller with the Manager
// The reconciler will be triggere by Gateway, or any dolphin-managed GatewayClass events
// Endpoints
func (r *gatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasMatchingControllerFn := hasMatchingController(context.Background(), r.Client, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		// its own resource
		For(&gatewayv1.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingControllerFn))).
		//GatewayClass resources, which are linked to Gateway
		Watches(&gatewayv1.GatewayClass{},
			r.enqueueRequestForOwningGatewayClass(),
			builder.WithPredicates(predicate.NewPredicateFuncs(matchesControllerName(controllerName)))).
		// Watch related LB service for status
		Watches(&corev1.Service{},
			r.enqueueRequestForOwningResource(),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
				_, found := object.GetLabels()[owningGatewayLabel]
				return found
			}))).
		// Watch HTTP Route status changes
		Watches(&gatewayv1.HTTPRoute{},
			r.enqueueRequestForOwningHTTPRoute(),
			builder.WithPredicates(onlyStatusChanged())).
		// Watch TLS Route status changes, there is one assumption that any change in spec will
		// always update status always at least for observedGeneration value.
		// Watches(&gatewayv1alpha2.TLSRoute{},
		// 	r.enqueueRequestForOwningTLSRoute(),
		// 	builder.WithPredicates(onlyStatusChanged())).
		// // Watch GRPCRoute status changes, there is one assumption that any change in spec will
		// // always update status always at least for observedGeneration value.
		// Watches(&gatewayv1alpha2.GRPCRoute{},
		// 	r.enqueueRequestForOwningGRPCRoute(),
		// 	builder.WithPredicates(onlyStatusChanged())).
		// Watch related secrets used to configure TLS
		// Watches(&corev1.Secret{},
		// 	r.enqueueRequestForTLSSecret(),
		// 	builder.WithPredicates(predicate.NewPredicateFuncs(r.usedInGateway))).
		// Watch related namespace in allowed namespaces
		Watches(&corev1.Namespace{},
			r.enqueueRequestForAllowedNamespace()).
		// Watch for changes to Reference Grants
		//Watches(&gatewayv1beta1.ReferenceGrant{}, r.enqueueRequestForReferenceGrant()).
		// Watch created and owned resources
		Owns(&dolphinv1.DolphinEnvoyConfig{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Endpoints{}).
		Complete(r)
}

// return an event handler for all Gateway objects belonging to the given GatewayClass
func (r *gatewayReconciler) enqueueRequestForOwningGatewayClass() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: gateway,
			logfields.Resource:   a.GetName(),
		})
		var reqs []reconcile.Request
		gwList := &gatewayv1.GatewayList{}
		if err := r.Client.List(ctx, gwList); err != nil {
			scopedLog.Error("Unable to list Gateways")
			return nil
		}

		for _, gw := range gwList.Items {
			if gw.Spec.GatewayClassName != gatewayv1.ObjectName(a.GetName()) {
				continue
			}
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gw.Namespace,
					Name:      gw.Name,
				},
			}
			reqs = append(reqs, req)
			scopedLog.WithFields(logrus.Fields{
				logfields.K8sNamespace: gw.GetNamespace(),
				logfields.Resource:     gw.GetName(),
			}).Info("Queueing gateway")
		}
		return reqs
	})
}

func (r *gatewayReconciler) enqueueRequestForOwningResource() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: "gateway",
			logfields.Resource:   a.GetName(),
		})

		key, found := a.GetLabels()[owningGatewayLabel]
		if !found {
			return nil
		}

		scopedLog.WithFields(logrus.Fields{
			logfields.K8sNamespace: a.GetNamespace(),
			logfields.Resource:     a.GetName(),
			"gateway":              key,
		}).Info("Enqueued gateway for owning service")

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: a.GetNamespace(),
					Name:      key,
				},
			},
		}
	})
}

func (r *gatewayReconciler) enqueueRequestForOwningHTTPRoute() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		hr, ok := a.(*gatewayv1.HTTPRoute)
		if !ok {
			return nil
		}

		return getReconcileRequestsForRoute(context.Background(), r.Client, a, hr.Spec.CommonRouteSpec)
	})
}

func getReconcileRequestsForRoute(ctx context.Context, c client.Client, object metav1.Object, route gatewayv1.CommonRouteSpec) []reconcile.Request {
	var reqs []reconcile.Request

	scopedLog := log.WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource: types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		},
	})

	for _, parent := range route.ParentRefs {
		if !helpers.IsGateway(parent) {
			continue
		}

		ns := helpers.NamespaceDerefOr(parent.Namespace, object.GetNamespace())

		gw := &gatewayv1.Gateway{}
		if err := c.Get(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      string(parent.Name),
		}, gw); err != nil {
			if !k8serrors.IsNotFound(err) {
				scopedLog.WithError(err).Error("Failed to get Gateway")
			}
			continue
		}

		if !hasMatchingController(ctx, c, controllerName)(gw) {
			scopedLog.Debug("Gateway does not have matching controller, skipping")
			continue
		}

		scopedLog.WithFields(logrus.Fields{
			logfields.K8sNamespace: ns,
			logfields.Resource:     parent.Name,
			logfields.Route:        object.GetName(),
		}).Info("Enqueued gateway for Route")

		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns,
				Name:      string(parent.Name),
			},
		})
	}

	return reqs
}

func (r *gatewayReconciler) enqueueRequestForAllowedNamespace() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, ns client.Object) []reconcile.Request {
		gateways := getGatewaysForNamespace(ctx, r.Client, ns)
		reqs := make([]reconcile.Request, 0, len(gateways))
		for _, gw := range gateways {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: gw,
			})
		}
		return reqs
	})
}
