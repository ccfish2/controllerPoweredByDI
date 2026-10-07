package gateway_api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	helpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	watchhandlers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/watch-handlers"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	mcsapiv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
)

type gatewayClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	controllerName   string
	logger           *slog.Logger
	cache            cache.Cache
	monitoredObjects []client.Object
}

func newGatewayClassReconciler(mgr ctrl.Manager, logger *slog.Logger, controllerName string, installedCRDs []schema.GroupVersionKind) *gatewayClassReconciler {
	monitoredObjects := []client.Object{
		&gatewayv1.GatewayClass{}, &dolphinv2alpha1.DolphinGatewayClassConfig{},
		&gatewayv1.Gateway{}, &gatewayv1.HTTPRoute{}, &gatewayv1.GRPCRoute{},
		&gatewayv1.ReferenceGrant{}, &gatewayv1.BackendTLSPolicy{},
		&corev1.Service{}, &corev1.Secret{}, &corev1.Namespace{}, &corev1.Node{},
		&discoveryv1.EndpointSlice{}, &dolphinv1.DolphinEnvoyConfig{},
	}
	for _, gvk := range installedCRDs {
		switch gvk.Kind {
		case helpers.TLSRouteKind:
			monitoredObjects = append(monitoredObjects, &gatewayv1.TLSRoute{})
		case helpers.TCPRouteKind:
			monitoredObjects = append(monitoredObjects, &gatewayv1.TCPRoute{})
		case helpers.ListenerSetKind:
			monitoredObjects = append(monitoredObjects, &gatewayv1.ListenerSet{})
		case helpers.ServiceImportKind:
			monitoredObjects = append(monitoredObjects, &mcsapiv1alpha1.ServiceImport{})
		}
	}
	return &gatewayClassReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), controllerName: controllerName,
		logger: logger, cache: mgr.GetCache(), monitoredObjects: monitoredObjects,
	}
}

func (r *gatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	setupStarted := time.Now()
	r.logger.Info("Registering GatewayClass controller with manager",
		slog.String("controllerName", r.controllerName),
		slog.String("watchKind", "GatewayClass"),
	)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("gatewayclass").
		WatchesRawSource(&loggedGatewayClassSource{
			name: "GatewayClass", logger: r.logger, cache: r.cache, monitoredObjects: r.monitoredObjects,
			source: source.TypedKind(mgr.GetCache(), &gatewayv1.GatewayClass{},
				&typedEventHandlerAdapter[*gatewayv1.GatewayClass]{delegate: &gatewayClassQueueEventHandler{
					logger:    r.logger,
					predicate: gatewayClassDebugPredicate(r.controllerName, r.logger),
				}}),
		}).
		WatchesRawSource(&loggedGatewayClassSource{
			name: "DolphinGatewayClassConfig", logger: r.logger, cache: r.cache, monitoredObjects: r.monitoredObjects,
			source: source.TypedKind(mgr.GetCache(), &dolphinv2alpha1.DolphinGatewayClassConfig{},
				&typedEventHandlerAdapter[*dolphinv2alpha1.DolphinGatewayClassConfig]{delegate: watchhandlers.EnqueueRequestForDolphinGatewayClassConfig(r.Client, r.logger)}),
		})

	if err := controllerBuilder.Complete(r); err != nil {
		r.logger.Error("GatewayClass controller setup FAILED", "error", err)
		return err
	}
	r.logger.Info("GatewayClass controller registration complete",
		"controllerName", r.controllerName,
		"watchKind", "GatewayClass",
		"duration", time.Since(setupStarted),
	)
	return nil
}

type typedEventHandlerAdapter[T client.Object] struct {
	delegate handler.EventHandler
}

func (a *typedEventHandlerAdapter[T]) Create(ctx context.Context, e event.TypedCreateEvent[T], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	a.delegate.Create(ctx, event.CreateEvent{Object: e.Object, IsInInitialList: e.IsInInitialList}, q)
}

func (a *typedEventHandlerAdapter[T]) Update(ctx context.Context, e event.TypedUpdateEvent[T], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	a.delegate.Update(ctx, event.UpdateEvent{ObjectOld: e.ObjectOld, ObjectNew: e.ObjectNew}, q)
}

func (a *typedEventHandlerAdapter[T]) Delete(ctx context.Context, e event.TypedDeleteEvent[T], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	a.delegate.Delete(ctx, event.DeleteEvent{Object: e.Object, DeleteStateUnknown: e.DeleteStateUnknown}, q)
}

func (a *typedEventHandlerAdapter[T]) Generic(ctx context.Context, e event.TypedGenericEvent[T], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	a.delegate.Generic(ctx, event.GenericEvent{Object: e.Object}, q)
}

// loggedGatewayClassSource reports each source's Start and cache-sync boundary.
// Controller-runtime starts workers only after every syncing source completes
// WaitForSync, so this identifies which watch (if any) prevents worker startup.
type loggedGatewayClassSource struct {
	name             string
	logger           *slog.Logger
	cache            cache.Cache
	monitoredObjects []client.Object
	source           source.TypedSyncingSource[reconcile.Request]
}

func (s *loggedGatewayClassSource) Start(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	started := time.Now()
	s.logger.InfoContext(ctx, "GatewayClass event source Start beginning", "source", s.name)
	if err := s.source.Start(ctx, q); err != nil {
		s.logger.ErrorContext(ctx, "GatewayClass event source Start failed", "source", s.name, "duration", time.Since(started), "error", err)
		return err
	}
	s.logger.InfoContext(ctx, "GatewayClass event source Start returned", "source", s.name, "duration", time.Since(started))
	return nil
}

func (s *loggedGatewayClassSource) WaitForSync(ctx context.Context) error {
	started := time.Now()
	s.logger.InfoContext(ctx, "GatewayClass event source cache sync waiting", "source", s.name)
	var monitorDone chan struct{}
	if s.name == "GatewayClass" {
		monitorDone = make(chan struct{})
		go s.monitorSharedCacheSync(ctx, monitorDone)
	}
	err := s.source.WaitForSync(ctx)
	if monitorDone != nil {
		close(monitorDone)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "GatewayClass event source cache sync failed", "source", s.name, "duration", time.Since(started), "error", err)
		return err
	}
	s.logger.InfoContext(ctx, "GatewayClass event source cache sync complete", "source", s.name, "duration", time.Since(started))
	return nil
}

func (s *loggedGatewayClassSource) monitorSharedCacheSync(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastStates := make(map[string]bool, len(s.monitoredObjects))
	lastReported := time.Now()
	for {
		unsynced := make([]string, 0)
		for _, obj := range s.monitoredObjects {
			name := fmt.Sprintf("%T", obj)
			informer, err := s.cache.GetInformer(ctx, obj, cache.BlockUntilSynced(false))
			if err != nil {
				name += " (get informer error: " + err.Error() + ")"
				unsynced = append(unsynced, name)
				continue
			}
			synced := informer.HasSynced()
			if previous, ok := lastStates[name]; !ok || previous != synced {
				s.logger.InfoContext(ctx, "Gateway shared-cache informer sync state", "informer", name, "synced", synced)
				lastStates[name] = synced
			}
			if !synced {
				unsynced = append(unsynced, name)
			}
		}
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if len(unsynced) > 0 && time.Since(lastReported) >= 10*time.Second {
				s.logger.WarnContext(ctx, "Gateway shared-cache informers still unsynced", "source", s.name, "unsyncedInformers", strings.Join(unsynced, ", "))
				lastReported = time.Now()
			}
		}
	}
}

// gatewayClassQueueEventHandler logs the source callback and queue insertion
// around the GatewayClass predicate. This separates informer/source delivery
// from controller queue/worker execution in debug builds.
type gatewayClassQueueEventHandler struct {
	logger    *slog.Logger
	predicate predicate.Predicate
}

func (h *gatewayClassQueueEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.handle(ctx, "create", e.Object, func() bool { return h.predicate.Create(e) }, q)
}

func (h *gatewayClassQueueEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	obj := e.ObjectNew
	if obj == nil {
		obj = e.ObjectOld
	}
	h.handle(ctx, "update", obj, func() bool { return h.predicate.Update(e) }, q)
}

func (h *gatewayClassQueueEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.handle(ctx, "delete", e.Object, func() bool { return h.predicate.Delete(e) }, q)
}

func (h *gatewayClassQueueEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.handle(ctx, "generic", e.Object, func() bool { return h.predicate.Generic(e) }, q)
}

func (h *gatewayClassQueueEventHandler) handle(ctx context.Context, eventType string, obj client.Object, accepted func() bool, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if obj == nil {
		h.logger.ErrorContext(ctx, "GatewayClass source handler received nil object", "eventType", eventType)
		return
	}
	key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	h.logger.InfoContext(ctx, "GatewayClass source handler received event",
		"eventType", eventType,
		"name", key.Name,
		"namespace", key.Namespace,
	)

	if !accepted() {
		h.logger.InfoContext(ctx, "GatewayClass event filtered by predicate",
			"eventType", eventType,
			"name", key.Name,
		)
		return
	}

	request := reconcile.Request{NamespacedName: key}
	h.logger.InfoContext(ctx, "GatewayClass reconcile request queue add starting",
		"eventType", eventType,
		"name", key.Name,
		"queueLengthBeforeAdd", q.Len(),
		"queueShuttingDown", q.ShuttingDown(),
	)
	q.Add(request)
	h.logger.InfoContext(ctx, "GatewayClass reconcile request queue add returned",
		"eventType", eventType,
		"name", key.Name,
		"queueLengthAfterAdd", q.Len(),
		"queueShuttingDown", q.ShuttingDown(),
	)
}

func gatewayClassDebugPredicate(controllerName string, logger *slog.Logger) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			logger.Info("GatewayClass create event received",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
				"controllerNameLabel", e.Object.GetLabels()["controllerName"],
			)
			matched := matchesControllerName(controllerName, logger, e.Object)
			logger.Info("GatewayClass create event match result",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
				"matched", matched,
			)
			if !matched {
				return false
			}
			logger.Info("GatewayClass predicate accepted create",
				"name", e.Object.GetName(),
				"controllerName", controllerName,
			)
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			logger.Info("GatewayClass delete event received",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
			)
			matched := matchesControllerName(controllerName, logger, e.Object)
			logger.Info("GatewayClass delete event match result",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
				"matched", matched,
			)
			if !matched {
				return false
			}
			logger.Info("GatewayClass predicate accepted delete",
				"name", e.Object.GetName(),
				"controllerName", controllerName,
			)
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			logger.Info("GatewayClass update event received",
				"oldName", e.ObjectOld.GetName(),
				"newName", e.ObjectNew.GetName(),
				"expectedControllerName", controllerName,
			)
			matched := matchesControllerName(controllerName, logger, e.ObjectNew)
			logger.Info("GatewayClass update event match result",
				"newName", e.ObjectNew.GetName(),
				"expectedControllerName", controllerName,
				"matched", matched,
			)
			if matched {
				logger.Info("GatewayClass predicate accepted update",
					"name", e.ObjectNew.GetName(),
					"controllerName", controllerName,
				)
			}
			return matched
		},
		GenericFunc: func(e event.GenericEvent) bool {
			logger.Info("GatewayClass generic event received",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
			)
			matched := matchesControllerName(controllerName, logger, e.Object)
			logger.Info("GatewayClass generic event match result",
				"name", e.Object.GetName(),
				"expectedControllerName", controllerName,
				"matched", matched,
			)
			if !matched {
				return false
			}
			logger.Info("GatewayClass predicate accepted generic event",
				"name", e.Object.GetName(),
				"controllerName", controllerName,
			)
			return true
		},
	}
}

func matchesControllerName(controllerName string, logger *slog.Logger, obj client.Object) bool {
	if obj == nil {
		if logger != nil {
			logger.Info("GatewayClass match check skipped: object is nil", "expectedControllerName", controllerName)
		}
		return false
	}

	gwc, ok := obj.(*gatewayv1.GatewayClass)
	if !ok || gwc == nil {
		if logger != nil {
			logger.Info("GatewayClass match check skipped: object is not a GatewayClass",
				"expectedControllerName", controllerName,
				"objType", fmt.Sprintf("%T", obj),
			)
		}
		return false
	}

	actualControllerName := string(gwc.Spec.ControllerName)
	match := actualControllerName == controllerName
	if logger != nil {
		logger.Info("GatewayClass controller match check",
			"name", gwc.Name,
			"expectedControllerName", controllerName,
			"actualControllerName", actualControllerName,
			"match", match,
		)
	} else {
		log.Info("GatewayClass controller match check",
			"name", gwc.Name,
			"expectedControllerName", controllerName,
			"actualControllerName", actualControllerName,
			"match", match,
		)
	}
	return match
}
