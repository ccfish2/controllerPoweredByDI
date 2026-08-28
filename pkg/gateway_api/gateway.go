package gateway_api

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/logging/logfields"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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
	mcsapiv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/indexers"
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

	// translator translation.Translator
	logger        *slog.Logger
	installedCRDs []schema.GroupVersionKind
}

func newGatewayReconciler(mgr ctrl.Manager, secretsNamespace string, idleTimeoutSeconds int, enableIpv4 bool, enableIpv6 bool, logger *slog.Logger, installedCRDs []schema.GroupVersionKind) *gatewayReconciler {
	scopedLog := logger.With(logfields.Controller, gateway)

	return &gatewayReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		IdleTimeoutSeconds: idleTimeoutSeconds,
		EnableIPv4:         enableIpv4,
		EnableIPv6:         enableIpv6,
		logger:             scopedLog,
		installedCRDs:      installedCRDs,
	}
}

// sets up the controller with the Manager
// The reconciler will be triggere by Gateway, or any dolphin-managed GatewayClass events
// Endpoints
func (r *gatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Determine which optional CRDs are enabled
	var tlsRouteEnabled, serviceImportEnabled bool

	for _, gvk := range r.installedCRDs {
		switch gvk.Kind {
		case helpers.TLSRouteKind:
			tlsRouteEnabled = true
		case helpers.ServiceImportKind:
			serviceImportEnabled = true
		}
	}

	// Add field indexes for HTTPRoutes
	for indexName, indexerFunc := range map[string]client.IndexerFunc{
		backendServiceHTTPRouteIndex: indexers.GenerateIndexerHTTPRouteByBackendService(r.Client, r.logger),
		gatewayHTTPRouteIndex:        indexers.IndexHTTPRouteByGateway,
	} {
		if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.HTTPRoute{}, indexName, indexerFunc); err != nil {
			return fmt.Errorf("failed to setup HTTPRoutes field indexer %q: %w", indexName, err)
		}
	}

	// Only index HTTPRoute by ServiceImport if ServiceImport is enabled
	if serviceImportEnabled {
		if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.HTTPRoute{}, backendServiceImportHTTPRouteIndex, indexers.IndexHTTPRouteByBackendServiceImport); err != nil {
			return fmt.Errorf("failed to setup HTTPRoute by ServiceImport field indexer %q: %w", backendServiceImportHTTPRouteIndex, err)
		}
	}

	// Index Gateways by implementation (ie `dolphin`)
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.Gateway{}, implementationGatewayIndex, indexers.GenerateIndexerGatewayByImplementation(r.Client, controllerName)); err != nil {
		return fmt.Errorf("failed to setup Gateways field indexer %q: %w", implementationGatewayIndex, err)
	}

	// Add indexes for TLSRoutes
	if tlsRouteEnabled {
		for indexName, indexerFunc := range map[string]client.IndexerFunc{
			backendServiceTLSRouteIndex: indexers.GenerateIndexerTLSRoutebyBackendService(r.Client, r.logger),
			gatewayTLSRouteIndex:        indexers.IndexTLSRouteByGateway,
		} {
			if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1alpha2.TLSRoute{}, indexName, indexerFunc); err != nil {
				return fmt.Errorf("failed to setup field indexer %q: %w", indexName, err)
			}
		}
	}

	// Add field indexes for GRPCRoutes
	for indexName, indexerFunc := range map[string]client.IndexerFunc{
		backendServiceGRPCRouteIndex: indexers.GenerateIndexerGRPCRoutebyBackendService(r.Client, r.logger),
		gatewayGRPCRouteIndex:        indexers.IndexGRPCRouteByGateway,
	} {
		if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.GRPCRoute{}, indexName, indexerFunc); err != nil {
			return fmt.Errorf("failed to setup TLSRoutes field indexer %q: %w", indexName, err)
		}
	}

	hasMatchingControllerFn := hasMatchingController(context.Background(), r.Client, controllerName, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	gatewayBuilder := ctrl.NewControllerManagedBy(mgr).
		// Watch its own resource
		For(&gatewayv1.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingControllerFn))).
		// Watch GatewayClass resources, which are linked to Gateway
		Watches(&gatewayv1.GatewayClass{},
			r.enqueueRequestForOwningGatewayClass(),
			builder.WithPredicates(predicate.NewPredicateFuncs(matchesControllerName(controllerName)))).
		// Watch related backend Service for status
		// LB Services are handled by the Owns call later.

		Watches(&corev1.Service{}, r.enqueueRequestForBackendService(tlsRouteEnabled)).
		// Watch HTTPRoute linked to Gateway
		Watches(&gatewayv1.HTTPRoute{}, r.enqueueRequestForOwningHTTPRoute(r.logger)).
		// Watch GRPCRoute linked to Gateway
		Watches(&gatewayv1.GRPCRoute{}, r.enqueueRequestForOwningGRPCRoute()).
		// Watch related secrets used to configure TLS
		Watches(&corev1.Secret{},
			r.enqueueRequestForTLSSecret(),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.usedInGateway))).
		// Watch related namespace in allowed namespaces
		Watches(&corev1.Namespace{},
			r.enqueueRequestForAllowedNamespace()).
		// Watch for changes to Reference Grants
		Watches(&gatewayv1beta1.ReferenceGrant{}, r.enqueueRequestForReferenceGrant()).
		Watches(&corev1.Node{}, r.enqueueRequestForNodes(r.Client, r.logger, owningGatewayLabel)).
		// Watch created and owned resources
		Owns(&dolphinv1.DolphinEnvoyConfig{}).
		Owns(&corev1.Service{}).
		Owns(&discoveryv1.EndpointSlice{})

	if tlsRouteEnabled {
		// Watch TLSRoute linked to Gateway
		gatewayBuilder = gatewayBuilder.Watches(&gatewayv1alpha2.TLSRoute{}, r.enqueueRequestForOwningTLSRoute(r.logger))
	}

	if serviceImportEnabled {
		// Watch for changes to Backend Service Imports
		gatewayBuilder = gatewayBuilder.Watches(&mcsapiv1alpha1.ServiceImport{}, r.enqueueRequestForBackendServiceImport())
	}

	return gatewayBuilder.Complete(r)
}

func (r *gatewayReconciler) usedInGateway(obj client.Object) bool {
	return len(getGatewaysForSecret(context.Background(), r.Client, obj, r.logger)) > 0
}

func (r *gatewayReconciler) enqueueRequestForBackendServiceImport() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		_, ok := o.(*mcsapiv1alpha1.ServiceImport)
		if !ok {
			return nil
		}

		scopedLog := r.logger.With(logfields.LogSubsys, "queue-gw-from-backend-svc-import")

		// make a set to hold all reconcile requests
		reconcileRequests := make(map[reconcile.Request]struct{})

		// Then, fetch all HTTPRoutes that reference this service, using the backendServiceIndex
		hrList := &gatewayv1.HTTPRouteList{}

		if err := r.Client.List(ctx, hrList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(backendServiceImportHTTPRouteIndex, client.ObjectKeyFromObject(o).String()),
		}); err != nil {
			scopedLog.ErrorContext(ctx, "Failed to get related HTTPRoutes", logfields.Error, err)
			return []reconcile.Request{}
		}

		// Fetch all the Dolphin-relevant Gateways using the implementationGatewayIndex.
		gwList := &gatewayv1.GatewayList{}
		if err := r.Client.List(ctx, gwList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(implementationGatewayIndex, "Dolphin"),
		}); err != nil {
			scopedLog.ErrorContext(ctx, "Failed to get Dolphin Gateways", logfields.Error, err)
			return []reconcile.Request{}
		}

		// Build a set of all Dolphin Gateway full names.
		// This makes sure we only add a reconcile.Request once for each Gateway.
		allDolphinGatewaysSet := make(map[string]struct{})
		for _, gw := range gwList.Items {
			gwFullName := types.NamespacedName{
				Name:      gw.GetName(),
				Namespace: gw.GetNamespace(),
			}
			allDolphinGatewaysSet[gwFullName.String()] = struct{}{}
		}

		// iterate through the HTTPRoutes, return a reconcile.Request for each Gateways that is relevant.
		for _, hr := range hrList.Items {
			for _, parent := range hr.Spec.ParentRefs {
				if !helpers.IsGateway(parent) {
					continue
				}
				parentFullName := types.NamespacedName{
					Name:      string(parent.Name),
					Namespace: helpers.NamespaceDerefOr(parent.Namespace, hr.Namespace),
				}
				if _, found := allDolphinGatewaysSet[parentFullName.String()]; found {
					reconcileRequests[reconcile.Request{NamespacedName: parentFullName}] = struct{}{}
				}
			}
		}

		// return the keys of the set.
		return slices.Collect(maps.Keys(reconcileRequests))
	})
}

// enqueueRequestForOwningTLSRoute returns an event handler that, when passed a TLSRoute, returns reconcile.Requests
// for all Dolphin-relevant Gateways associated with that TLSRoute.
func (r *gatewayReconciler) enqueueRequestForOwningTLSRoute(logger *slog.Logger) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		hr, ok := a.(*gatewayv1alpha2.TLSRoute)
		if !ok {
			return nil
		}

		return getGatewayReconcileRequestsForRoute(context.Background(), r.Client, a, hr.Spec.CommonRouteSpec, logger)
	})
}

func getGatewayReconcileRequestsForRoute(ctx context.Context, c client.Client, object metav1.Object, route gatewayv1.CommonRouteSpec, logger *slog.Logger) []reconcile.Request {
	var reqs []reconcile.Request

	scopedLog := logger.With(
		logfields.Resource, types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		},
	)

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
				scopedLog.ErrorContext(ctx, "Failed to get Gateway", logfields.Error, err)
			}
			continue
		}

		if !hasMatchingController(ctx, c, controllerName, slog.New(slog.NewTextHandler(os.Stdout, nil)))(gw) {
			scopedLog.DebugContext(ctx, "Gateway does not have matching controller, skipping")
			continue
		}

		scopedLog.InfoContext(ctx,
			"Enqueued gateway for Route",
			logfields.K8sNamespace, ns,
			logfields.ParentResource, parent.Name,
			logfields.Route, object.GetName())

		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns,
				Name:      string(parent.Name),
			},
		})
	}

	return reqs
}

func (r *gatewayReconciler) enqueueRequestForNodes(c client.Client, logger *slog.Logger, owningGatewayLabel string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, ns client.Object) []reconcile.Request {
		scopedLog := logger.With(
			logfields.K8sNamespace, ns.GetName(),
		)
		nodeList := &corev1.NodeList{}
		if err := c.List(ctx, nodeList); err != nil {
			scopedLog.WarnContext(ctx, "Unable to list nodes", logfields.Error, err)
			return nil
		}

		gateways, err := getAllDolphinGatewaysSet(ctx, c)

		if err != nil {
			scopedLog.ErrorContext(ctx, "Failed to get Dolphin Gateways", logfields.Error, err)
			return []reconcile.Request{}
		}

		reqs := make([]reconcile.Request, 0, len(gateways))
		svcList := &corev1.ServiceList{}
		svcMap := make(map[string]struct{})

		// for each gateway, filter for the services owned by the gateway
		for gw := range gateways {
			gwSplit := strings.SplitN(gw, "/", 2)
			gwName := gwSplit[1]
			if err := c.List(ctx, svcList, client.MatchingLabels{
				owningGatewayLabel: gwName,
			}); err != nil {
				scopedLog.WarnContext(ctx, "Unable to list services", logfields.Error, err)
			}
			// if the service owned by the gateway is a nodeport, add to map of UID
			for _, svc := range svcList.Items {
				if svc.Spec.Type == "NodePort" {
					svcMap[string(svc.GetOwnerReferences()[0].UID)] = struct{}{}
				}
			}
		}

		// queue up a request for every Dolphin related gateway
		for gw := range gateways {
			gwSplit := strings.SplitN(gw, "/", 2)

			if len(gwSplit) != 2 {
				scopedLog.ErrorContext(ctx, "Failed to get namespace name", logfields.Error, err)
				return []reconcile.Request{}
			}

			gwNamespace, gwName := gwSplit[0], gwSplit[1]

			gatewayNamespaceName := types.NamespacedName{
				Namespace: gwNamespace,
				Name:      gwName,
			}

			gateway := &gatewayv1.Gateway{}

			if err := c.Get(ctx, gatewayNamespaceName, gateway); err != nil {
				scopedLog.WarnContext(ctx, "Unable to get gateway", logfields.Error, err)
			}
			if _, err := svcMap[string(gateway.GetUID())]; err {
				// there is no nodeport svc for this gateway, no need to reconcile
				continue
			}
			reqs = append(reqs, reconcile.Request{
				NamespacedName: gatewayNamespaceName,
			})
		}
		return reqs
	})
}

func (r *gatewayReconciler) enqueueRequestForReferenceGrant() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueAll())
}

// updateReconcileRequestsForParentRefs mutates the passed reconcile.Request set to add all
func updateReconcileRequestsForParentRefs(parentRefs []gatewayv1.ParentReference, ns string, allGatewaysSet map[string]struct{}, rrSet map[reconcile.Request]struct{}) {
	for _, parent := range parentRefs {
		if !helpers.IsGateway(parent) {
			continue
		}
		parentFullName := types.NamespacedName{
			Name:      string(parent.Name),
			Namespace: helpers.NamespaceDerefOr(parent.Namespace, ns),
		}
		if _, found := allGatewaysSet[parentFullName.String()]; found {
			rrSet[reconcile.Request{NamespacedName: parentFullName}] = struct{}{}
		}
	}
}

func (r *gatewayReconciler) enqueueAll() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := r.logger.With(
			logfields.Resource, client.ObjectKeyFromObject(o),
		)
		list := &gatewayv1.GatewayList{}

		if err := r.Client.List(ctx, list, &client.ListOptions{}); err != nil {
			scopedLog.ErrorContext(ctx, "Failed to list Gateway", logfields.Error, err)
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(list.Items))
		for _, item := range list.Items {
			gw := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: gw,
			})
			scopedLog.InfoContext(ctx, "Enqueued Gateway for resource", gateway, gw)
		}
		return requests
	}
}

// enqueueRequestForOwningGRPCRoute returns an event handler that, when passed a GRPCRoute, returns reconcile.Requests
// for any Dolphin-relevant Gateways associated with that GRPCRoute.
func (r *gatewayReconciler) enqueueRequestForOwningGRPCRoute() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		gr, ok := a.(*gatewayv1.GRPCRoute)
		if !ok {
			return nil
		}

		return getGatewayReconcileRequestsForRoute(ctx, r.Client, a, gr.Spec.CommonRouteSpec, r.logger)
	})
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

func (r *gatewayReconciler) enqueueRequestForOwningHTTPRoute(logger *slog.Logger) handler.EventHandler {
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
		if !hasMatchingController(ctx, c, controllerName, slog.New(slog.NewTextHandler(os.Stdout, nil)))(gw) {
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

// enqueueRequestForTLSSecret returns an event handler for any changes with TLS secrets
func (r *gatewayReconciler) enqueueRequestForTLSSecret() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		gateways := getGatewaysForSecret(ctx, r.Client, a, r.logger)
		reqs := make([]reconcile.Request, 0, len(gateways))
		for _, gw := range gateways {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gw.GetNamespace(),
					Name:      gw.GetName(),
				},
			})
		}
		return reqs
	})
}

func (r *gatewayReconciler) enqueueRequestForBackendService(tlsRouteEnabled bool) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		_, ok := o.(*corev1.Service)
		if !ok {
			return nil
		}

		scopedLog := r.logger.With(logfields.LogSubsys, "queue-gw-from-backend-svc")

		// Make a set to hold all reconcile requests.
		reconcileRequests := make(map[reconcile.Request]struct{})

		// Fetch all HTTPRoutes that reference this Service.
		hrList := &gatewayv1.HTTPRouteList{}
		if err := r.Client.List(ctx, hrList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				backendServiceHTTPRouteIndex,
				client.ObjectKeyFromObject(o).String(),
			),
		}); err != nil {
			scopedLog.ErrorContext(ctx, "Failed to get related HTTPRoutes", logfields.Error, err)
			return []reconcile.Request{}
		}

		// Fetch all TLSRoutes only when the TLSRoute CRD/index is enabled.
		tlsrList := &gatewayv1alpha2.TLSRouteList{}
		if tlsRouteEnabled {
			if err := r.Client.List(ctx, tlsrList, &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector(
					backendServiceTLSRouteIndex,
					client.ObjectKeyFromObject(o).String(),
				),
			}); err != nil {
				scopedLog.ErrorContext(ctx, "Failed to get related TLSRoutes", logfields.Error, err)
				return []reconcile.Request{}
			}
		}

		// Fetch all GRPCRoutes that reference this Service.
		grpcRouteList := &gatewayv1.GRPCRouteList{}
		if err := r.Client.List(ctx, grpcRouteList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				backendServiceGRPCRouteIndex,
				client.ObjectKeyFromObject(o).String(),
			),
		}); err != nil {
			scopedLog.ErrorContext(ctx, "Unable to list GRPCRoutes", logfields.Error, err)
			return []reconcile.Request{}
		}

		// Fetch all Dolphin-relevant Gateways.
		gwList := &gatewayv1.GatewayList{}
		if err := r.Client.List(ctx, gwList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				implementationGatewayIndex,
				"dolphin",
			),
		}); err != nil {
			scopedLog.ErrorContext(ctx, "Failed to get Dolphin Gateways", logfields.Error, err)
			return []reconcile.Request{}
		}

		// Build a set of all Dolphin Gateway full names.
		allDolphinGatewaysSet := make(map[string]struct{})

		for _, gw := range gwList.Items {
			gwFullName := types.NamespacedName{
				Name:      gw.GetName(),
				Namespace: gw.GetNamespace(),
			}
			allDolphinGatewaysSet[gwFullName.String()] = struct{}{}
		}

		// Add Gateways referenced by HTTPRoutes.
		for _, hr := range hrList.Items {
			updateReconcileRequestsForParentRefs(
				hr.Spec.ParentRefs,
				hr.Namespace,
				allDolphinGatewaysSet,
				reconcileRequests,
			)
		}

		// Add Gateways referenced by TLSRoutes.
		if tlsRouteEnabled {
			for _, tlsr := range tlsrList.Items {
				updateReconcileRequestsForParentRefs(
					tlsr.Spec.ParentRefs,
					tlsr.Namespace,
					allDolphinGatewaysSet,
					reconcileRequests,
				)
			}
		}

		// Add Gateways referenced by GRPCRoutes.
		for _, grpcr := range grpcRouteList.Items {
			updateReconcileRequestsForParentRefs(
				grpcr.Spec.ParentRefs,
				grpcr.Namespace,
				allDolphinGatewaysSet,
				reconcileRequests,
			)
		}

		// Return the keys of the set.
		return slices.Collect(maps.Keys(reconcileRequests))
	})
}
