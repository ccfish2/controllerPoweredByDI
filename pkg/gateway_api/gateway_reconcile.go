package gateway_api

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	routechecks "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/routechecker"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8serros "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	//myself
	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	gatewayapihelpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	policychecks "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/policychecks"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/ingestion"
	translation "github.com/ccfish2/controllerPoweredByDI/pkg/model/translation/gateway-api"

	// dolphin
	"sync/atomic"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

// Add near the package-level declarations:
var activeGatewayReconciles atomic.Int64

type gatewayDiagnosticContextKey struct{}

type gatewayPhaseTracker struct {
	logger *slog.Logger
	name   string
	start  time.Time
}

func (p *gatewayPhaseTracker) set(name string) {
	now := time.Now()
	if p.name != "" {
		p.logger.Info("Gateway reconcile phase finished",
			"phase", p.name,
			"duration", now.Sub(p.start),
		)
	}
	p.name = name
	p.start = now
	p.logger.Info("Gateway reconcile phase started", "phase", name)
}

func (p *gatewayPhaseTracker) finish() {
	if p.name != "" {
		p.logger.Info("Gateway reconcile phase finished",
			"phase", p.name,
			"duration", time.Since(p.start),
		)
	}
}

type gatewayDiagnosticClient struct {
	client.Client
	logger *slog.Logger
}

func (c *gatewayDiagnosticClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	started := time.Now()
	phase := "unknown"
	if p, ok := ctx.Value(gatewayDiagnosticContextKey{}).(*gatewayPhaseTracker); ok && p.name != "" {
		phase = p.name
	}

	c.logger.InfoContext(ctx, "Gateway client Get starting",
		"phase", phase, "key", key.String(), "objectType", fmt.Sprintf("%T", obj))

	err := c.Client.Get(ctx, key, obj, opts...)
	c.logger.InfoContext(ctx, "Gateway client Get returned",
		"phase", phase, "key", key.String(), "objectType", fmt.Sprintf("%T", obj),
		"duration", time.Since(started), "error", err, "contextError", ctx.Err())
	return err
}

func (c *gatewayDiagnosticClient) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	started := time.Now()
	phase := "unknown"
	if p, ok := ctx.Value(gatewayDiagnosticContextKey{}).(*gatewayPhaseTracker); ok && p.name != "" {
		phase = p.name
	}

	c.logger.InfoContext(ctx, "Gateway client List starting",
		"phase", phase, "listType", fmt.Sprintf("%T", list))

	err := c.Client.List(ctx, list, opts...)
	c.logger.InfoContext(ctx, "Gateway client List returned",
		"phase", phase, "listType", fmt.Sprintf("%T", list),
		"duration", time.Since(started), "error", err, "contextError", ctx.Err())
	return err
}

func (r *gatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reconcileStarted := time.Now()
	diagnosticLogger := r.logger
	if diagnosticLogger == nil {
		diagnosticLogger = slog.Default()
	}
	diagnosticLogger.Info("Gateway reconcile started", "namespace", req.Namespace, "name", req.Name)
	active := activeGatewayReconciles.Add(1)
	diagnosticLogger.Info("Gateway reconcile worker entered",
		"namespace", req.Namespace,
		"name", req.Name,
		"activeGatewayReconciles", active,
	)
	defer func() {
		active := activeGatewayReconciles.Add(-1)
		diagnosticLogger.Info("Gateway reconcile finished",
			"namespace", req.Namespace,
			"name", req.Name,
			"duration", time.Since(reconcileStarted),
			"activeGatewayReconciles", active,
		)
	}()

	tracker := &gatewayPhaseTracker{logger: diagnosticLogger}
	ctx = context.WithValue(ctx, gatewayDiagnosticContextKey{}, tracker)
	defer tracker.finish()

	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info("Reconciling Gateway")

	// step 1: retrieve the gateway
	gw := &gatewayv1.Gateway{}
	tracker.set("get Gateway")
	err := r.Client.Get(ctx, req.NamespacedName, gw)
	if err != nil {
		if k8serros.IsNotFound(err) {
			scopedLog.Info("Gateway not found")
			return ctrl.Result{}, nil
		}

		scopedLog.WithError(err).Error("Failed to get Gateway")
		return ctrl.Result{}, err
	}

	scopedLog.WithFields(logrus.Fields{
		"gatewayClassName": gw.Spec.GatewayClassName,
		"generation":       gw.Generation,
		"resourceVersion":  gw.ResourceVersion,
	}).Info("Gateway fetched")

	// ignore deleting gateway
	if gw.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	copy := gw.DeepCopy()

	// step 2: Gather all required information for the ingestion model
	gwc := &gatewayv1.GatewayClass{}
	tracker.set("get GatewayClass")
	err = r.Client.Get(ctx,
		client.ObjectKey{Name: string(gw.Spec.GatewayClassName)},
		gwc,
	)

	if err != nil {
		scopedLog.WithFields(logrus.Fields{
			"gatewayClassName": gw.Spec.GatewayClassName,
			"error":            err,
		}).Error("Unable to get GatewayClass Reconcile")

		return controllerruntime.Success()
	}

	scopedLog.WithFields(logrus.Fields{
		"gatewayClassName": gwc.Name,
		"controllerName":   gwc.Spec.ControllerName,
	}).Info("GatewayClass fetched")

	// handle HTTPRouteList, TLSRouteList, ServiceList
	if string(gwc.Spec.ControllerName) != controllerName {
		scopedLog.Debug("GatewayClass does not have matching controller name, doing nothing")
		return controllerruntime.Success()
	}
	tracker.set("list HTTPRoutes")
	httpRouteList := &gatewayv1.HTTPRouteList{}
	err = r.Client.List(ctx, httpRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("list TLSRoutes")
	tlsRouteList := &gatewayv1.TLSRouteList{}
	err = r.Client.List(ctx, tlsRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("list Services")
	servicesList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, servicesList); err != nil {
		scopedLog.WithError(err).Error("Unable to list Services")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("list GRPCRoutes")
	grpcRouteList := &gatewayv1.GRPCRouteList{}
	if err := r.Client.List(ctx, grpcRouteList); err != nil {
		scopedLog.Error(ctx, "Unable to list GRPCRoutes", logfields.Error, err)
		return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
	}

	tcpRouteList := &gatewayv1.TCPRouteList{}
	if r.hasInstalledCRD(helpers.TCPRouteKind) {
		tracker.set("list TCPRoutes")
		if err := r.Client.List(ctx, tcpRouteList);
		// 	 &client.ListOptions{
		// 	FieldSelector: fields.OneTermEqualSelector(indexers.GatewayTCPRouteIndex, client.ObjectKeyFromObject(original).String()),
		// });
		err != nil {
			scopedLog.Error(ctx, "Unable to list TCPRoutes", logfields.Error, err)
			return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
		}
	}

	grants := &gatewayv1.ReferenceGrantList{}
	tracker.set("list ReferenceGrantList")
	if err := r.Client.List(ctx, grants); err != nil {
		scopedLog.Error(ctx, "Unable to list ReferenceGrants", logfields.Error, err)
		return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
	}

	// filterTCPRoutesByGateway reads TCPRoute status, so refresh it first.
	if r.hasInstalledCRD(helpers.TCPRouteKind) {
		tracker.set("update TCPRoute statuses")
		if err := r.setTCPRouteStatuses(r.logger, ctx, tcpRouteList, grants); err != nil {
			scopedLog.Error(ctx, "Unable to update TCPRoute Status", logfields.Error, err)
			return controllerruntime.Fail(err)
		}
	}

	var attachedListenerSets []gatewayv1.ListenerSet
	if helpers.HasListenerSetSupport(r.Client.Scheme()) {
		tracker.set("list ListenerSets")
		listenerSets, err := r.listenerSetsForGateway(ctx, gw)
		if err != nil {
			scopedLog.Error(ctx, "Unable to list ListenerSets", logfields.Error, err)
			return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
		}
		tracker.set("filter allowed ListenerSets")
		attachedListenerSets = r.filterToAllowedListenerSets(ctx, scopedLog, gw, listenerSets)
	}
	tracker.set("merge listeners and find conflicts")
	listenerContexts := r.mergeListeners(ctx, scopedLog, gw, attachedListenerSets)
	conflictedListeners := conflictsAcrossSources(listenerContexts)

	var namespaces []corev1.Namespace
	if hasAllowedRoutesNamespaceSelector(gw) {
		namespaceList := &corev1.NamespaceList{}
		tracker.set("list Namespaces")
		if err := r.Client.List(ctx, namespaceList); err != nil {
			scopedLog.Error(ctx, "Unable to list Namespaces", logfields.Error, err)
			return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
		}
		namespaces = namespaceList.Items
	}
	tracker.set("build namespace index and filter routes")
	namespaceLabels := helpers.NewNamespaceLabelIndex(namespaces)
	dgccfg := r.getGatewayClassConfig(ctx, gwc)
	HTTPRoutes := r.filterHTTPRoutesByGateway(ctx, copy, httpRouteList.Items)
	tcpRoutes := r.filterTCPRoutesByGateway(ctx, gw, attachedListenerSets, tcpRouteList.Items)
	tracker.set("build ingestion model")
	httpListeners, tlsListeners, tcpListeners := ingestion.GatewayAPI(ingestion.Input{
		GatewayClass:       *gwc,
		Gateway:            *copy,
		GatewayClassConfig: dgccfg,
		HTTPRoutes:         HTTPRoutes,
		TLSRoutes:          r.filterTLSRoutesByGateway(ctx, copy, tlsRouteList.Items),
		GRPCRoutes:         r.filterGRPCRoutesByGateway(ctx, gw, grpcRouteList.Items, namespaceLabels),
		TCPRoutes:          tcpRoutes,
		Services:           servicesList.Items,
	})

	btlspList := &gatewayv1.BackendTLSPolicyList{}
	tracker.set("list BackendTLSPolicies")
	if err := r.Client.List(ctx, btlspList); err != nil {
		scopedLog.WithError(err).Error("Unable to list BackendTLSPolicies")
		return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
	}
	if len(btlspList.Items) > 0 {
		tracker.set("update BackendTLSPolicy statuses")
		btlspMap := helpers.BuildBackendTLSPolicyLookup(btlspList)
		if err := r.setBackendTLSPolicyStatuses(&slog.Logger{}, ctx, HTTPRoutes, btlspMap, req.NamespacedName); err != nil {
			scopedLog.WithError(err).Error("Unable to update BackendTLSPolicy Status")
			return controllerruntime.Fail(err)
		}
	}

	tracker.set("update Gateway listener statuses")
	err = r.setListenerStatus(ctx, copy, httpRouteList, tlsRouteList, grpcRouteList, namespaceLabels)
	if err != nil {
		scopedLog.WithError(err).Error("Unable to set listener status")
		setGatewayAccepted(copy, false, "Unable to set listener status")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	setGatewayAccepted(copy, true, "Gateway successfully scheduled")

	// ListenerSet status is reported independently from the parent Gateway's
	// Accepted and Programmed conditions. Those Gateway conditions reflect the
	// Gateway's local configuration, so valid ListenerSets do not make an
	// otherwise invalid Gateway accepted or programmed.
	tracker.set("update ListenerSet statuses")
	r.setListenerSetStatuses(
		ctx,
		gw,
		attachedListenerSets,
		conflictedListeners,
		httpRouteList,
		tlsRouteList,
		grpcRouteList,
		tcpRouteList,
		nil,
		namespaceLabels,
	)

	// step 3: translate the listeners into dolphin model
	tracker.set("translate listeners")
	trans := translation.NewTranslator(r.SecretNamespace, r.IdleTimeoutSeconds, true, false)
	dec, svc, ep, err := trans.Translate(&model.Model{HTTP: httpListeners, TLS: tlsListeners, TCP: tcpListeners}, dgccfg)
	if err != nil {
		scopedLog.WithError(err).Error("Unable to translate resources")
		setGatewayAccepted(gw, false, "Unable to translate resources")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	var translatedPorts []string
	if svc != nil {
		for _, port := range svc.Spec.Ports {
			translatedPorts = append(translatedPorts,
				fmt.Sprintf("%s:%d/%s", port.Name, port.Port, port.Protocol))
		}
	}
	scopedLog.WithFields(logrus.Fields{
		"httpListenerCount": len(httpListeners),
		"tlsListenerCount":  len(tlsListeners),
		"servicePorts":      translatedPorts,
	}).Info("Gateway translation result")

	tracker.set("ensure Service")
	if err := r.ensureService(ctx, svc); err != nil {
		scopedLog.WithError(err).Error("Unable to create Service")
		setGatewayAccepted(gw, false, "Unable to create Service resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("ensure Endpoints")
	if err := r.ensureEndpoints(ctx, ep); err != nil {
		scopedLog.WithError(err).Error("Unable to ensure Endpoints")
		setGatewayAccepted(gw, false, "Unable to ensure Endpoints resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("ensure DolphinEnvoyConfig")
	if err := r.ensureEnvoyConfig(ctx, dec); err != nil {
		scopedLog.WithError(err).Error("Unable to ensure DolphinEnvoyConfig")
		setGatewayAccepted(gw, false, "Unable to ensure CEC resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	// step 4: update the status of the gateway
	tracker.set("set Gateway address")
	if err := r.setAddressStatus(ctx, copy); err != nil {
		scopedLog.WithError(err).Error("Address is not ready")
		setGatewayProgrammed(gw, false, "Address is not ready")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tracker.set("update Gateway status")
	setGatewayProgrammed(copy, true, "reconciled successfully")
	if err := r.updateStatus(ctx, gw, copy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update Gateway status: %w", err)
	}
	scopedLog.Info("Successfully reconciled Gateway")
	return reconcile.Result{}, nil
}

// acceptedListeners is an ordered accumulator of listeners that have already
// won their port. Listeners are checked against it in precedence order, so an
// earlier listener keeps the port and a later conflicting one is rejected.
type acceptedListeners struct {
	listeners []gatewayv1.Listener
}

func (a *acceptedListeners) checkConflict(l gatewayv1.Listener) gatewayv1.ListenerConditionReason {
	for i := range a.listeners {
		if reason, ok := listenerPairConflict(&a.listeners[i], &l); ok {
			return reason
		}
	}
	return ""
}

func (a *acceptedListeners) accept(l gatewayv1.Listener) {
	a.listeners = append(a.listeners, l)
}

func conflictsAcrossSources(listeners []ingestion.ListenerWithContext) listenerConflictsBySource {
	listenersBySource := make(map[model.FullyQualifiedResource][]gatewayv1.Listener)
	var sources []model.FullyQualifiedResource
	for _, listener := range listeners {
		if _, knownSource := listenersBySource[listener.Source]; !knownSource {
			sources = append(sources, listener.Source)
		}
		listenersBySource[listener.Source] = append(listenersBySource[listener.Source], listener.Listener)
	}

	conflicts := make(listenerConflictsBySource)
	accepted := &acceptedListeners{}
	for _, source := range sources {
		var eligible []gatewayv1.Listener

		for _, listener := range listenersBySource[source] {

			// Find conflicts with any earlier accepted listener.
			//
			// The earlier, higher precedence, listener which conflicts is
			// already in the accepted set
			if reason := accepted.checkConflict(listener); reason != "" {
				if conflicts[source] == nil {
					conflicts[source] = map[gatewayv1.SectionName]listenerConflict{}
				}
				conflicts[source][listener.Name] = listenerConflict{reason: reason}
				continue
			}

			eligible = append(eligible, listener)
		}

		// Find conflicts within the source.
		//
		// Such conflicts never enter the accepted set
		for name, conflict := range conflictsWithinSource(eligible) {
			if conflicts[source] == nil {
				conflicts[source] = map[gatewayv1.SectionName]listenerConflict{}
			}
			conflicts[source][name] = conflict
		}

		for _, listener := range eligible {
			if _, conflicted := conflicts[source][listener.Name]; !conflicted {
				accepted.accept(listener)
			}
		}
	}
	return conflicts
}

type listenerValidationParams struct {
	ownerNamespace string
	ownerKind      string
	generation     int64
	grants         []gatewayv1.ReferenceGrant
	ownerRef       string
}

type listenerValidationResult struct {
	isValid         bool
	supportedKinds  []gatewayv1.RouteGroupKind
	invalidReason   gatewayv1.ListenerConditionReason
	invalidMessages []string
	conds           []metav1.Condition
}

func listenerInvalidRouteKinds(generation int64, msg string) metav1.Condition {
	return metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionFalse,
		Reason:             string(gatewayv1.ListenerReasonInvalidRouteKinds),
		Message:            msg,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

// listenerPairConflict reports whether two listeners that share a Gateway, or a
// Gateway and its ListenerSets, conflict, along with the reason. Listeners on
// different ports never conflict.
func listenerPairConflict(first, second *gatewayv1.Listener) (gatewayv1.ListenerConditionReason, bool) {
	if first.Port != second.Port {
		return "", false
	}

	// firstL4 := isL4Protocol(first.Protocol)
	// secondL4 := isL4Protocol(second.Protocol)

	// // L4 listeners own a port outright with no demultiplexing. TCP and UDP on
	// // the same port are the only compatible case involving an L4 listener.
	// if firstL4 || secondL4 {
	// 	if firstL4 && secondL4 && first.Protocol != second.Protocol {
	// 		return "", false
	// 	}
	// 	return gatewayv1.ListenerReasonProtocolConflict, true
	// }

	// // HTTPS termination and TLS passthrough both consume the SNI of the same
	// // port, so they conflict whenever their hostnames can match the same value.
	// if isHTTPSAndTLSPassthroughPair(first, second) {
	// 	if helpers.SNIHostnamesIntersect(
	// 		helpers.ListenerHostname(first), helpers.ListenerHostname(second)) {
	// 		return gatewayv1.ListenerReasonProtocolConflict, true
	// 	}
	// 	return "", false
	// }

	// // Listeners of the same muxed protocol demultiplex by hostname, so they only
	// // conflict when they claim the exact same hostname.
	// if first.Protocol == second.Protocol &&
	// 	normalizedListenerHostname(first) == normalizedListenerHostname(second) {
	// 	return gatewayv1.ListenerReasonHostnameConflict, true
	// }

	return "", false
}

func listenerConflictMessage(
	reason gatewayv1.ListenerConditionReason,
	self, other *gatewayv1.Listener,
) string {
	switch {
	case reason == gatewayv1.ListenerReasonHostnameConflict:
		return fmt.Sprintf(
			"Listener conflicts with listener %q: same port %d has overlapping hostnames.",
			other.Name, self.Port)
	case isHTTPSAndTLSPassthroughPair(self, other):
		return fmt.Sprintf(
			"Listener conflicts with listener %q: same port %d has overlapping HTTPS and TLS passthrough hostnames.",
			other.Name, self.Port)
	default:
		return fmt.Sprintf(
			"Listener conflicts with listener %q: same port %d has incompatible protocols.",
			other.Name, self.Port)
	}
}

func isHTTPSAndTLSPassthroughPair(first, second *gatewayv1.Listener) bool {
	return (helpers.IsHTTPSTerminatedListener(first) && helpers.IsTLSPassthroughListener(second)) ||
		(helpers.IsHTTPSTerminatedListener(second) && helpers.IsTLSPassthroughListener(first))
}

func conflictsWithinSource(listeners []gatewayv1.Listener) map[gatewayv1.SectionName]listenerConflict {
	conflicts := map[gatewayv1.SectionName]listenerConflict{}

	for i := range listeners {
		for j := i + 1; j < len(listeners); j++ {
			first := &listeners[i]
			second := &listeners[j]
			reason, ok := listenerPairConflict(first, second)
			if !ok {
				continue
			}

			conflicts[first.Name] = listenerConflict{
				reason:  reason,
				message: listenerConflictMessage(reason, first, second),
			}
			conflicts[second.Name] = listenerConflict{
				reason:  reason,
				message: listenerConflictMessage(reason, second, first),
			}
		}
	}

	return conflicts
}

func (r *gatewayReconciler) validateListener(ctx context.Context, l gatewayv1.Listener, params listenerValidationParams) listenerValidationResult {
	res := listenerValidationResult{
		isValid:       true,
		invalidReason: gatewayv1.ListenerReasonInvalid,
	}

	allSupported := getSupportedRouteKinds(l.Protocol)
	if allSupported == nil {
		res.invalidMessages = append(res.invalidMessages, "Unsupported Listener Protocol.")
		res.invalidReason = gatewayv1.ListenerReasonUnsupportedProtocol
		res.isValid = false
	}

	// if r.hostNetworkEnabled && isL4Protocol(l.Protocol) {
	// 	res.invalidMessages = append(res.invalidMessages,
	// 		fmt.Sprintf("%s listeners are not supported when Gateway API Host Network mode is enabled", l.Protocol))
	// 	res.invalidReason = gatewayv1.ListenerReasonUnsupportedProtocol
	// 	res.isValid = false
	// 	return res
	// }

	if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) > 0 {
		res.supportedKinds = []gatewayv1.RouteGroupKind{}
		for _, supported := range allSupported {
			for _, allowed := range l.AllowedRoutes.Kinds {
				if supported.Kind == allowed.Kind &&
					groupDerefOr(allowed.Group, gatewayv1.GroupName) == string(*supported.Group) {
					res.supportedKinds = append(res.supportedKinds, supported)
					break
				}
			}
		}

		if len(res.supportedKinds) != len(l.AllowedRoutes.Kinds) {
			res.conds = merge(res.conds, listenerInvalidRouteKinds(params.generation, "Unsupported Route Kinds in allowedRoutes.kinds"))

			if len(res.supportedKinds) == 0 {
				res.invalidMessages = append(res.invalidMessages, "None of the Allowed Route Kinds are supported.")
				res.isValid = false
			}
		}
	} else {
		res.supportedKinds = allSupported
	}

	if l.TLS != nil {
		ownerGVK := helpers.GatewayV1GVK(params.ownerKind)
		for _, cert := range l.TLS.CertificateRefs {
			if !helpers.IsSecret(cert) {
				res.conds = merge(res.conds, metav1.Condition{
					Type:               string(gatewayv1.ListenerConditionResolvedRefs),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
					Message:            "Invalid CertificateRef",
					ObservedGeneration: params.generation,
					LastTransitionTime: metav1.Now(),
				})
				res.invalidMessages = append(res.invalidMessages, "Invalid CertificateRef, must be a Secret.")
				res.isValid = false
				break
			}

			if !helpers.IsSecretReferenceAllowed(params.ownerNamespace, cert, ownerGVK, params.grants) {
				res.conds = merge(res.conds, metav1.Condition{
					Type:               string(gatewayv1.ListenerConditionResolvedRefs),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.ListenerReasonRefNotPermitted),
					Message:            "CertificateRef is not permitted",
					ObservedGeneration: params.generation,
					LastTransitionTime: metav1.Now(),
				})
				res.invalidMessages = append(res.invalidMessages, "Invalid CertificateRef, not permitted.")
				res.isValid = false
				break
			}

			if err := validateTLSSecret(ctx, r.Client, helpers.NamespaceDerefOr(cert.Namespace, params.ownerNamespace), string(cert.Name)); err != nil {
				r.logger.InfoContext(ctx, "Found an invalid TLS Secret",
					logfields.Error, err.Error(),
					logfields.Resource, params.ownerRef)
				res.conds = merge(res.conds, metav1.Condition{
					Type:               string(gatewayv1.ListenerConditionResolvedRefs),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
					Message:            "Invalid CertificateRef",
					ObservedGeneration: params.generation,
					LastTransitionTime: metav1.Now(),
				})
				res.invalidMessages = append(res.invalidMessages, "Invalid CertificateRef, "+err.Error())
				res.isValid = false
				break
			}
		}
		// Handle terminated TLSRoute until we support it
		if l.Protocol == gatewayv1.TLSProtocolType && l.TLS.Mode != nil && *l.TLS.Mode == gatewayv1.TLSModeTerminate {
			// Until we support this, we need to mark this as invalid.
			res.isValid = false
			res.invalidMessages = append(res.invalidMessages, "Using TLSRoute with TLS.mode Terminate is unsupported.")
			res.invalidReason = gatewayv1.ListenerReasonUnsupportedValue
			// The specific conformance test for this expects supportedKinds to be empty.
			// This is probably an upstream bug, but work around it for now.
			res.supportedKinds = []gatewayv1.RouteGroupKind{}
		}
	}

	return res
}

type listenerConflict struct {
	reason  gatewayv1.ListenerConditionReason
	message string
}

type listenerConflictsBySource map[model.FullyQualifiedResource]map[gatewayv1.SectionName]listenerConflict

func (r *gatewayReconciler) filterTCPRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, attachedListenerSets []gatewayv1.ListenerSet, routes []gatewayv1.TCPRoute) []gatewayv1.TCPRoute {
	var filtered []gatewayv1.TCPRoute
	for _, route := range routes {
		if helpers.IsParentAttachable(ctx, gw, &route, route.Status.Parents, attachedListenerSets) {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (r *gatewayReconciler) setListenerSetStatuses(
	ctx context.Context,
	gw *gatewayv1.Gateway,
	attachedListenerSets []gatewayv1.ListenerSet,
	conflictedListeners listenerConflictsBySource,
	httpRoutes *gatewayv1.HTTPRouteList,
	tlsRoutes *gatewayv1.TLSRouteList,
	grpcRoutes *gatewayv1.GRPCRouteList,
	tcpRoutes *gatewayv1.TCPRouteList,
	udpRoutes *gatewayv1.UDPRouteList,
	namespaceLabels helpers.NamespaceLabelIndex,
) {
	gw.Status.AttachedListenerSets = nil

	grants := &gatewayv1.ReferenceGrantList{}
	if err := r.Client.List(ctx, grants); err != nil {
		r.logger.ErrorContext(ctx, "Failed to list ReferenceGrants for ListenerSet status", logfields.Error, err)
		return
	}

	var validAttachedCount int32
	for i := range attachedListenerSets {
		ls := &attachedListenerSets[i]
		original := ls.DeepCopy()
		lsSource := listenerSetFQR(ls)

		oneValidListener := false
		var listenerStatuses []gatewayv1.ListenerEntryStatus

		for _, entry := range ls.Spec.Listeners {
			l := helpers.ListenerEntryToListener(entry)
			var conds []metav1.Condition

			_, isConflicted := conflictedListeners[lsSource][l.Name]

			// if isConflicted {
			// 	conds = merge(conds,
			// 		listenerAcceptedCondition(ls.GetGeneration(), false, conflict.reason, "Listener has a conflict"),
			// 		listenerProgrammedCondition(ls.GetGeneration(), false, conflict.reason, "Listener has a conflict"),
			// 		listenerConflictedCondition(ls.GetGeneration(), conflict.reason, "Listener has a conflict"),
			// 		metav1.Condition{
			// 			Type:               string(gatewayv1.ListenerConditionResolvedRefs),
			// 			Status:             metav1.ConditionTrue,
			// 			Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
			// 			Message:            "Resolved Refs",
			// 			ObservedGeneration: ls.GetGeneration(),
			// 			LastTransitionTime: metav1.Now(),
			// 		},
			// 	)
			// }

			var supportedKinds []gatewayv1.RouteGroupKind
			if !isConflicted {
				res := r.validateListener(ctx, l, listenerValidationParams{
					ownerNamespace: ls.Namespace,
					ownerKind:      ls.Kind,
					generation:     ls.GetGeneration(),
					grants:         grants.Items,
					ownerRef:       client.ObjectKeyFromObject(ls).String(),
				})
				isValid := res.isValid
				supportedKinds = res.supportedKinds
				conds = merge(conds, res.conds...)

				if !isValid {
					conds = merge(conds,
						listenerAcceptedCondition(ls.GetGeneration(), false, res.invalidReason, "Listener not valid. "+strings.Join(res.invalidMessages, " ")),
						listenerProgrammedCondition(ls.GetGeneration(), false, res.invalidReason, "Listener not valid"),
					)
				} else {
					oneValidListener = true

					// If ResolvedRefs is not already present, add a successful one.
					if !helpers.IsConditionPresent(conds, string(gatewayv1.ListenerConditionResolvedRefs)) {
						conds = merge(conds, metav1.Condition{
							Type:               string(gatewayv1.ListenerConditionResolvedRefs),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
							Message:            "Resolved Refs",
							ObservedGeneration: ls.GetGeneration(),
							LastTransitionTime: metav1.Now(),
						})
					}
					conds = merge(conds,
						listenerAcceptedCondition(ls.GetGeneration(), true, gatewayv1.ListenerReasonAccepted, "Listener Accepted"),
						listenerProgrammedCondition(ls.GetGeneration(), true, gatewayv1.ListenerConditionReason(gatewayv1.ListenerConditionProgrammed), "Listener Programmed"),
					)
				}
			}

			var attachedRoutes int32
			attachedRoutes += int32(len(r.filterHTTPRoutesByListener(ctx, gw, &l, &lsSource, httpRoutes.Items, namespaceLabels, *ls)))
			attachedRoutes += int32(len(r.filterTCPRoutesByListener(ctx, gw, &l, &lsSource, tcpRoutes.Items, namespaceLabels, *ls)))

			listenerStatuses = append(listenerStatuses, gatewayv1.ListenerEntryStatus{
				Name:           entry.Name,
				SupportedKinds: supportedKinds,
				Conditions:     conds,
				AttachedRoutes: attachedRoutes,
			})
		}

		ls.Status.Listeners = listenerStatuses

		if oneValidListener {
			validAttachedCount++
			setListenerSetAccepted(ls, true, "ListenerSet is accepted", gatewayv1.ListenerSetReasonAccepted)
			setListenerSetProgrammed(ls, true, "ListenerSet is programmed", gatewayv1.ListenerSetReasonProgrammed)
		} else {
			setListenerSetAccepted(ls, false, "No valid listeners", gatewayv1.ListenerSetReasonListenersNotValid)
			setListenerSetProgrammed(ls, false, "No valid listeners", gatewayv1.ListenerSetReasonListenersNotValid)
		}

		if err := r.updateListenerSetStatus(ctx, original, ls); err != nil {
			r.logger.ErrorContext(ctx, "Unable to update ListenerSet status", logfields.Error, err,
				logfields.Resource, client.ObjectKeyFromObject(ls).String())
		}
	}

	if validAttachedCount > 0 {
		gw.Status.AttachedListenerSets = &validAttachedCount
	}
}

func (r *gatewayReconciler) setTCPRouteStatuses(scopedLog *slog.Logger, ctx context.Context, tcpRoutes *gatewayv1.TCPRouteList, grants *gatewayv1.ReferenceGrantList) error {
	scopedLog.Debug("Updating TCPRoute statuses for Gateway", numRoutes, len(tcpRoutes.Items))
	for tcpRouteIndex, original := range tcpRoutes.Items {
		tcpr := original.DeepCopy()
		tcpr.Status.Parents = pruneRouteParentStatuses(tcpr.Status.Parents, tcpr.Spec.ParentRefs, "io.dolphin/gateway-controller")

		i := &routechecks.TCPRouteInput{
			Ctx:            ctx,
			Logger:         log,
			Client:         r.Client,
			Grants:         grants,
			TCPRoute:       tcpr,
			ControllerName: "io.dolphin/gateway-controller",
		}

		if err := r.runCommonRouteChecks(ctx, i, tcpr.Spec.ParentRefs, tcpr.Namespace); err != nil {
			return fmt.Errorf("failure during TCPRoute checks: %w", err)
		}

		if err := r.updateTCPRouteStatus(ctx, scopedLog, &original, tcpr); err != nil {
			return fmt.Errorf("failed to update TCPRoute status: %w", err)
		}

		tcpRoutes.Items[tcpRouteIndex].Status = tcpr.Status
	}

	return nil
}

// runCommonRouteChecks runs all the checks that are common across all supported Route types.
//
// Uses the helpers.Input interface to ensure that this still applies as new types are added.
func (r *gatewayReconciler) runCommonRouteChecks(ctx context.Context, input routechecks.Input, parentRefs []gatewayv1.ParentReference, objNamespace string) error {
	for _, parent := range parentRefs {
		if helpers.IsGateway(parent) {
			if err := r.runGatewayRouteChecks(ctx, input, parent, objNamespace); err != nil {
				return err
			}
		} else if helpers.IsListenerSet(parent) {
			// if err := r.runListenerSetRouteChecks(ctx, input, parent, objNamespace); err != nil {
			// 	return err
			// }
		}
	}

	return nil
}

func (r *gatewayReconciler) runGatewayRouteChecks(ctx context.Context, input routechecks.Input, parent gatewayv1.ParentReference, objNamespace string) error {
	if !r.parentIsMatchingGateway(ctx, parent, objNamespace) {
		return nil
	}
	if !r.checkRouteSupported(input, parent) {
		return nil
	}

	setInitialRouteConditions(input, parent)
	// if err := runCheckFuncs(input, parent, gatewayCheckFuncs, "Gateway"); err != nil {
	// 	return err
	// }
	return nil
	//return runCheckFuncs(input, parent, backendCheckFuncs, "Backend")
}

var gatewayCheckFuncs = []routechecks.CheckWithParentFunc{
	routechecks.CheckGatewayMatchingProtocol,
	routechecks.CheckGatewayRouteKindAllowed,
	routechecks.CheckGatewayMatchingPorts,
	routechecks.CheckGatewayMatchingHostnames,
	routechecks.CheckGatewayMatchingSection,
	routechecks.CheckGatewayAllowedForNamespace,
}

var backendCheckFuncs = []routechecks.CheckWithParentFunc{
	routechecks.CheckAgainstCrossNamespaceBackendReferences_V2,
	routechecks.CheckBackend,
	routechecks.CheckHasServiceImportSupport,
	routechecks.CheckBackendIsExistingService_V2,
}

func runCheckFuncs(input routechecks.Input, parent gatewayv1.ParentReference, fns []routechecks.CheckWithParentFunc, errPrefix string) error {
	for _, fn := range fns {
		continueCheck, err := fn(input, parent)
		if err != nil {
			return fmt.Errorf("failed to apply %s check: %w", errPrefix, err)
		}
		if !continueCheck {
			break
		}
	}
	return nil
}
func setInitialRouteConditions(input routechecks.Input, parent gatewayv1.ParentReference) {
	input.SetParentCondition(parent, metav1.Condition{
		Type:    string(gatewayv1.RouteConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.RouteReasonAccepted),
		Message: fmt.Sprintf("Accepted %s", input.GetGVK().Kind),
	})
	input.SetParentCondition(parent, metav1.Condition{
		Type:    string(gatewayv1.RouteConditionResolvedRefs),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.RouteReasonResolvedRefs),
		Message: "Service reference is valid",
	})
}

// checkRouteSupported returns false when route validation should stop for this input.
func (r *gatewayReconciler) checkRouteSupported(input routechecks.Input, parent gatewayv1.ParentReference) bool {
	switch k := input.GetGVK().Kind; k {
	case kindTCPRoute, kindUDPRoute:
		if r.hostNetworkEnabled {
			input.SetParentCondition(parent, metav1.Condition{
				Type:    string(gatewayv1.RouteConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.RouteReasonUnsupportedValue),
				Message: fmt.Sprintf("%s is not supported when Gateway API Host Network mode is enabled", k),
			})
			input.SetParentCondition(parent, metav1.Condition{
				Type:    string(gatewayv1.RouteConditionResolvedRefs),
				Status:  metav1.ConditionUnknown,
				Reason:  string(gatewayv1.RouteReasonPending),
				Message: "Backend references were not evaluated because this route type is not supported in Gateway API Host Network mode",
			})
			return false
		}
	}

	return true
}

func (r *gatewayReconciler) parentIsMatchingGateway(ctx context.Context, parent gatewayv1.ParentReference, namespace string) bool {
	hasMatchingControllerFn := helpers.GatewayHasMatchingControllerFn(ctx, r.Client, "io.dolphin/gateway-controller", r.logger)
	if !helpers.IsGateway(parent) {
		return false
	}
	gw := &gatewayv1.Gateway{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: helpers.NamespaceDerefOr(parent.Namespace, namespace),
		Name:      string(parent.Name),
	}, gw); err != nil {
		return false
	}
	return hasMatchingControllerFn(gw)
}

func (r *gatewayReconciler) updateTCPRouteStatus(ctx context.Context, scopedLog *slog.Logger, original *gatewayv1.TCPRoute, new *gatewayv1.TCPRoute) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, lastTransitionTime)) {
		return nil
	}
	scopedLog.Debug("Updating TCPRoute status", tcpRoute, types.NamespacedName{Name: original.Name, Namespace: original.Namespace})
	return r.Client.Status().Update(ctx, new)
}

func (r *gatewayReconciler) filterTCPRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, listenerSource *model.FullyQualifiedResource, routes []gatewayv1.TCPRoute, namespaceLabels helpers.NamespaceLabelIndex, attachedListenerSets ...gatewayv1.ListenerSet) []gatewayv1.TCPRoute {
	_ = listenerOwnerNamespace(gw, listenerSource)
	var filtered []gatewayv1.TCPRoute
	for _, route := range routes {
		if helpers.IsParentAttachable(ctx, gw, &route, route.Status.Parents, attachedListenerSets) &&
			listenerisAllowed(gw, listener, &route, namespaceLabels) &&
			parentRefMatched(gw, listener, route.GetNamespace(), route.Spec.ParentRefs) {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func listenerOwnerNamespace(gw *gatewayv1.Gateway, listenerSource *model.FullyQualifiedResource) string {
	if listenerSource != nil && listenerSource.Kind == "ListenerSet" {
		return listenerSource.Namespace
	}
	return gw.GetNamespace()
}

// following three should be verified using local run
func (r *gatewayReconciler) ensureEnvoyConfig(ctx context.Context, desired *dolphinv1.DolphinEnvoyConfig) error {
	dec := desired.DeepCopy()
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, dec, func() error {
		dec.Spec = desired.Spec
		setMergedLabelsAndAnnotations(dec, desired)
		return nil
	})
	return err
}

func (r *gatewayReconciler) ensureEndpoints(ctx context.Context, desired *corev1.Endpoints) error {
	ep := desired.DeepCopy()
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, ep, func() error {
		ep.Subsets = desired.Subsets
		ep.OwnerReferences = desired.OwnerReferences
		setMergedLabelsAndAnnotations(ep, desired)
		return nil
	})
	return err
}

func (r *gatewayReconciler) ensureService(ctx context.Context, desired *corev1.Service) error {
	svc := desired.DeepCopy()
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, svc, func() error {
		lbClass := desired.Spec.LoadBalancerClass
		svc.Spec = desired.Spec
		svc.OwnerReferences = desired.OwnerReferences
		setMergedLabelsAndAnnotations(svc, desired)
		svc.Spec.LoadBalancerClass = lbClass
		return nil
	})
	return err
}

func (r *gatewayReconciler) getGatewayClassConfig(ctx context.Context, gwc *gatewayv1.GatewayClass) *dolphinv2alpha1.DolphinGatewayClassConfig {
	if gwc.Spec.ParametersRef == nil ||
		gwc.Spec.ParametersRef.Group != dolphinv2alpha1.CustomResourceDefinitionGroup ||
		gwc.Spec.ParametersRef.Kind != dolphinv2alpha1.DGCCKindDefinition {
		return nil
	}

	res := &dolphinv2alpha1.DolphinGatewayClassConfig{}
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: string(*gwc.Spec.ParametersRef.Namespace),
		Name:      gwc.Spec.ParametersRef.Name,
	}, res); err != nil {
		return nil
	}
	return res
}

func hasAllowedRoutesNamespaceSelector(gw *gatewayv1.Gateway) bool {
	for _, listener := range gw.Spec.Listeners {
		if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil {
			continue
		}
		if listener.AllowedRoutes.Namespaces.From != nil && *listener.AllowedRoutes.Namespaces.From == gatewayv1.NamespacesFromSelector {
			return true
		}
		if listener.AllowedRoutes.Namespaces.From == nil && listener.AllowedRoutes.Namespaces.Selector != nil {
			return true
		}
	}
	return false
}

// audit gateway routes configuration
// calculate and update the statistics into the GW status
// update collecting listeners info from gateway and update the total routes
func (r *gatewayReconciler) setListenerStatus(ctx context.Context, gw *gatewayv1.Gateway, httpRoutes *gatewayv1.HTTPRouteList, tlsRoutes *gatewayv1.TLSRouteList, grpcRoutes *gatewayv1.GRPCRouteList, namespaceLabels helpers.NamespaceLabelIndex) error {
	grants := &gatewayv1.ReferenceGrantList{}
	if err := r.Client.List(ctx, grants); err != nil {
		return fmt.Errorf("failed to retrieve reference grants: %w", err)
	}

	for _, l := range gw.Spec.Listeners {
		isValid := true

		// SupportedKinds is a required field, so we can't declare it as nil.
		supportedKinds := []gatewayv1.RouteGroupKind{}
		invalidRouteKinds := false
		protoGroup, protoKind := getSupportedGroupKind(l.Protocol)

		if l.AllowedRoutes != nil && len(l.AllowedRoutes.Kinds) != 0 {
			for _, k := range l.AllowedRoutes.Kinds {
				if groupDerefOr(k.Group, gatewayv1.GroupName) == string(*protoGroup) &&
					k.Kind == protoKind {
					supportedKinds = append(supportedKinds, k)
				} else {
					invalidRouteKinds = true
				}
			}
		} else {
			g, k := getSupportedGroupKind(l.Protocol)
			supportedKinds = []gatewayv1.RouteGroupKind{
				{
					Group: g,
					Kind:  k,
				},
			}
		}
		var conds []metav1.Condition
		if invalidRouteKinds {
			conds = append(conds, gatewayListenerInvalidRouteKinds(gw, "Invalid Route Kinds"))
			isValid = false
		} else {
			conds = append(conds, gatewayListenerProgrammedCondition(gw, true, "Listener Programmed"))
			conds = append(conds, gatewayListenerAcceptedCondition(gw, true, "Listener Accepted"))
			conds = append(conds, metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
				Message:            "Resolved Refs",
				LastTransitionTime: metav1.Now(),
			})
		}

		if l.TLS != nil {
			for _, cert := range l.TLS.CertificateRefs {
				if !helpers.IsSecret(cert) {
					conds = merge(conds, metav1.Condition{
						Type:               string(gatewayv1.ListenerConditionResolvedRefs),
						Status:             metav1.ConditionFalse,
						Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
						Message:            "Invalid CertificateRef",
						LastTransitionTime: metav1.Now(),
					})
					isValid = false
					break
				}

				if !helpers.IsSecretReferenceAllowed(gw.Namespace, cert, gatewayv1.SchemeGroupVersion.WithKind("Gateway"), grants.Items) {
					conds = merge(conds, metav1.Condition{
						Type:               string(gatewayv1.ListenerConditionResolvedRefs),
						Status:             metav1.ConditionFalse,
						Reason:             string(gatewayv1.ListenerReasonRefNotPermitted),
						Message:            "CertificateRef is not permitted",
						LastTransitionTime: metav1.Now(),
					})
					isValid = false
					break
				}

				if err := validateTLSSecret(ctx, r.Client, helpers.NamespaceDerefOr(cert.Namespace, gw.GetNamespace()), string(cert.Name)); err != nil {
					conds = merge(conds, metav1.Condition{
						Type:               string(gatewayv1.ListenerConditionResolvedRefs),
						Status:             metav1.ConditionFalse,
						Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
						Message:            "Invalid CertificateRef",
						LastTransitionTime: metav1.Now(),
					})
					isValid = false
					break
				}
			}
		}

		if !isValid {
			conds = merge(conds, metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalid),
				Message:            "Invalid CertificateRef",
				LastTransitionTime: metav1.Now(),
			})
		}
		gwSource := gatewayFQR(gw)
		var attachedRoutes int32
		attachedRoutes += int32(len(r.filterHTTPRoutesByListener(ctx, gw, &l, &gwSource, httpRoutes.Items, namespaceLabels)))
		attachedRoutes += int32(len(r.filterTLSRoutesByListener(ctx, gw, &l, tlsRoutes.Items)))
		attachedRoutes += int32(len(r.filterGRPCRoutesByListener(ctx, gw, &l, grpcRoutes.Items, namespaceLabels)))

		found := false
		for i := range gw.Status.Listeners {
			if l.Name == gw.Status.Listeners[i].Name {
				found = true
				gw.Status.Listeners[i].SupportedKinds = supportedKinds
				gw.Status.Listeners[i].Conditions = conds
				gw.Status.Listeners[i].AttachedRoutes = attachedRoutes
				break
			}
		}
		if !found {
			gw.Status.Listeners = append(gw.Status.Listeners, gatewayv1.ListenerStatus{
				Name:           l.Name,
				SupportedKinds: supportedKinds,
				Conditions:     conds,
				AttachedRoutes: attachedRoutes,
			})
		}
	}

	// filter listener status to only have active listeners
	var newListenersStatus []gatewayv1.ListenerStatus
	for _, ls := range gw.Status.Listeners {
		for _, l := range gw.Spec.Listeners {
			if ls.Name == l.Name {
				newListenersStatus = append(newListenersStatus, ls)
				break
			}
		}
	}
	gw.Status.Listeners = newListenersStatus
	return nil
}

func (r *gatewayReconciler) filterGRPCRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routes []gatewayv1.GRPCRoute, namespaceLabels helpers.NamespaceLabelIndex) []gatewayv1.GRPCRoute {
	var filtered []gatewayv1.GRPCRoute
	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) &&
			listenerisAllowed(gw, listener, &route, namespaceLabels) &&
			len(computeHostsForListener(listener, route.Spec.Hostnames)) > 0 &&
			parentRefMatched(gw, listener, route.GetNamespace(), route.Spec.ParentRefs) {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

// read and compare pem
func validateTLSSecret(ctx context.Context, c client.Client, namespace, name string) error {
	secret := corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, &secret)
	if err != nil {
		return err
	}
	if !isValidPemFormat(secret.Data[corev1.TLSCertKey]) {
		return fmt.Errorf("tls cert key is missed")
	} else if !isValidPemFormat(secret.Data[corev1.TLSPrivateKeyKey]) {
		return fmt.Errorf("TLS PRIVATE KEY ")
	}
	return nil
}

// check ObjectMeta and conditions
func isRouteMatchGateway(gw *gatewayv1.Gateway, route metav1.Object, parents []gatewayv1.RouteParentStatus) bool {
	for _, rps := range parents {
		if helpers.NamespaceDerefOr(rps.ParentRef.Namespace, route.GetNamespace()) != gw.Namespace ||
			string(rps.ParentRef.Name) != gw.GetName() {
			continue
		}

		for _, cond := range rps.Conditions {
			if cond.Type == string(gatewayv1.RouteConditionAccepted) && cond.Status == metav1.ConditionTrue {
				return true
			}
			if cond.Type == string(gatewayv1.RouteConditionResolvedRefs) && cond.Status == metav1.ConditionFalse {
				return true
			}
		}
	}
	return false
}

// it is the configuration allowed
// permited
func (r *gatewayReconciler) filterHTTPRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, listenerSource *model.FullyQualifiedResource, routes []gatewayv1.HTTPRoute, namespaceLabels helpers.NamespaceLabelIndex, attachedListenerSets ...gatewayv1.ListenerSet) []gatewayv1.HTTPRoute {
	var filtered []gatewayv1.HTTPRoute
	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) &&
			isAllowed(ctx, r.Client, gw, &route) &&
			len(computeHostsForListener(listener, route.Spec.Hostnames)) > 0 &&
			parentRefMatched(gw, listener, route.GetNamespace(), route.Spec.ParentRefs) {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

// permited, and matched
func (r *gatewayReconciler) filterTLSRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routes []gatewayv1.TLSRoute, attachedListenerSets ...gatewayv1.ListenerSet) []gatewayv1.TLSRoute {
	var filtered []gatewayv1.TLSRoute
	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) &&
			isAllowed(ctx, r.Client, gw, &route) &&
			len(computeHostsForListener(listener, route.Spec.Hostnames)) > 0 &&
			parentRefMatched(gw, listener, route.GetNamespace(), route.Spec.ParentRefs) {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (r *gatewayReconciler) listenerSetsForGateway(
	ctx context.Context,
	gw *gatewayv1.Gateway,
) ([]gatewayv1.ListenerSet, error) {
	lsList := &gatewayv1.ListenerSetList{}
	if err := r.Client.List(ctx, lsList);
	//  &client.ListOptions{
	// 	FieldSelector: fields.OneTermEqualSelector(indexers.ListenerSetGatewayIndex, client.ObjectKeyFromObject(gw).String()),
	// }
	err != nil {
		return nil, fmt.Errorf("failed to list ListenerSets: %w", err)
	}

	sortListenerSets(lsList.Items)
	return lsList.Items, nil
}

// sortListenerSets sorts ListenerSets by precedence rules
func sortListenerSets(sets []gatewayv1.ListenerSet) {
	sort.Slice(sets, func(i, j int) bool {
		ti := sets[i].CreationTimestamp.Time
		tj := sets[j].CreationTimestamp.Time
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		ni := sets[i].GetNamespace() + "/" + sets[i].GetName()
		nj := sets[j].GetNamespace() + "/" + sets[j].GetName()
		return ni < nj
	})
}

func (r *gatewayReconciler) filterToAllowedListenerSets(
	ctx context.Context,
	scopedLog *logrus.Entry,
	gw *gatewayv1.Gateway,
	listenerSets []gatewayv1.ListenerSet,
) []gatewayv1.ListenerSet {
	var allowed []gatewayv1.ListenerSet
	for i := range listenerSets {
		ls := &listenerSets[i]
		if !helpers.IsListenerSetAllowed(ctx, r.Client, gw, ls, scopedLog) {
			original := ls.DeepCopy()
			setListenerSetAccepted(ls, false, "ListenerSet is not allowed by the Gateway's allowedListeners policy", gatewayv1.ListenerSetReasonNotAllowed)
			setListenerSetProgrammed(ls, false, "ListenerSet is not allowed by the Gateway's allowedListeners policy", gatewayv1.ListenerSetReasonNotAllowed)
			if err := r.updateListenerSetStatus(ctx, original, ls); err != nil {
				scopedLog.Error(ctx, "Unable to update ListenerSet status", logfields.Error, err)
			}
			continue
		}
		allowed = append(allowed, *ls)
	}
	return allowed
}

func (r *gatewayReconciler) updateListenerSetStatus(ctx context.Context, original *gatewayv1.ListenerSet, new *gatewayv1.ListenerSet) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, lastTransitionTime)) {
		return nil
	}
	return r.Client.Status().Update(ctx, new)
}

func (r *gatewayReconciler) mergeListeners(
	ctx context.Context,
	scopedLog *logrus.Entry,
	gw *gatewayv1.Gateway,
	listenerSets []gatewayv1.ListenerSet,
) []ingestion.ListenerWithContext {
	gwSource := gatewayFQR(gw)

	var merged []ingestion.ListenerWithContext
	for _, listener := range gw.Spec.Listeners {
		merged = append(merged, ingestion.ListenerWithContext{
			Listener:         listener,
			Source:           gwSource,
			SourceGeneration: gw.Generation,
		})
	}

	for i := range listenerSets {
		ls := &listenerSets[i]
		lsSource := listenerSetFQR(ls)
		for _, entry := range ls.Spec.Listeners {
			listener := helpers.ListenerEntryToListener(entry)
			merged = append(merged, ingestion.ListenerWithContext{
				Listener:          listener,
				Source:            lsSource,
				SourceGeneration:  ls.Generation,
				AllowedNamespaces: resolveAllowedNamespaces(ctx, r.Client, ls.GetNamespace(), listener, scopedLog),
			})
		}
	}

	return merged
}

// resolveAllowedNamespaces resolves a listener's allowedRoutes.namespaces policy
// into a set of namespace names. Returns nil to indicate all namespaces are allowed.
func resolveAllowedNamespaces(ctx context.Context, c client.Client, listenerNamespace string, listener gatewayv1.Listener, logger *logrus.Entry) map[string]struct{} {
	if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil || listener.AllowedRoutes.Namespaces.From == nil {
		return map[string]struct{}{listenerNamespace: {}}
	}
	switch *listener.AllowedRoutes.Namespaces.From {
	case gatewayv1.NamespacesFromAll:
		return nil
	case gatewayv1.NamespacesFromSame:
		return map[string]struct{}{listenerNamespace: {}}
	case gatewayv1.NamespacesFromSelector:
		nsList := &corev1.NamespaceList{}
		selector, _ := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
		if err := c.List(ctx, nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			logger.Error(ctx, "Unable to list namespaces for listener", logfields.Error, err)
			return map[string]struct{}{listenerNamespace: {}}
		}
		allowed := make(map[string]struct{})
		for _, ns := range nsList.Items {
			allowed[ns.Name] = struct{}{}
		}
		return allowed
	}
	return map[string]struct{}{listenerNamespace: {}}
}

func parentRefMatched(gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routeNamespace string, parefs []gatewayv1.ParentReference) bool {
	for _, ref := range parefs {
		if string(ref.Name) == gw.GetName() && gw.GetNamespace() == helpers.NamespaceDerefOr(ref.Namespace, routeNamespace) {
			if ref.SectionName == nil && ref.Port == nil {
				return true
			}
			sectionNameCheck := ref.SectionName == nil || *ref.SectionName == listener.Name
			portCheck := ref.Port == nil || *ref.Port == listener.Port
			if sectionNameCheck && portCheck {
				return true
			}
		}
	}
	return false
}

// isAllowed returns true if the provided Route is allowed to attach to given gateway
func isGRPCAllowed(gw *gatewayv1.Gateway, route metav1.Object, namespaceLabels gatewayapihelpers.NamespaceLabelIndex) bool {
	for _, listener := range gw.Spec.Listeners {
		if listenerisAllowed(gw, &listener, route, namespaceLabels) {
			return true
		}
	}
	return false
}

// isAllowed returns true if the provided Route is allowed to attach to given gateway
func isAllowed(ctx context.Context, c client.Client, gw *gatewayv1.Gateway, route metav1.Object) bool {
	for _, listener := range gw.Spec.Listeners {
		// all routes in the same namespace are allowed for this listener
		if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil {
			return route.GetNamespace() == gw.GetNamespace()
		}

		// check if route is kind-allowed
		if !isKindAllowed(listener, route) {
			continue
		}

		// check if route is namespace-allowed
		switch *listener.AllowedRoutes.Namespaces.From {
		case gatewayv1.NamespacesFromAll:
			return true
		case gatewayv1.NamespacesFromSame:
			if route.GetNamespace() == gw.GetNamespace() {
				return true
			}
		case gatewayv1.NamespacesFromSelector:
			nsList := &corev1.NamespaceList{}
			selector, _ := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
			if err := c.List(ctx, nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
				log.WithError(err).Error("Unable to list namespaces")
				return false
			}

			for _, ns := range nsList.Items {
				if ns.Name == route.GetNamespace() {
					return true
				}
			}
		}
	}
	return false
}

func (r *gatewayReconciler) filterGRPCRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1.GRPCRoute, namespaceLabels helpers.NamespaceLabelIndex) []gatewayv1.GRPCRoute {
	var filtered []gatewayv1.GRPCRoute

	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) && isGRPCAllowed(gw, &route, namespaceLabels) && len(computeHosts(gw, route.Spec.Hostnames)) > 0 {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

// this enables running locally: either ingress or hostip would work on gateway-api
func (r *gatewayReconciler) setAddressStatus(ctx context.Context, gw *gatewayv1.Gateway) error {
	svcList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, svcList, client.MatchingLabels{
		owningGatewayLabel: model.Shorten(gw.GetName()),
	}, client.InNamespace(gw.GetNamespace())); err != nil {
		return err
	}

	if len(svcList.Items) == 0 {
		return fmt.Errorf("no service found")
	}

	svc := svcList.Items[0]
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return fmt.Errorf("load balancer status is not ready")
	}

	var addresses []gatewayv1.GatewayStatusAddress
	for _, s := range svc.Status.LoadBalancer.Ingress {
		if len(s.IP) != 0 {
			addresses = append(addresses, gatewayv1.GatewayStatusAddress{
				Type:  GatewayAddressTypePtr(gatewayv1.IPAddressType),
				Value: s.IP,
			})
		}
		if len(s.Hostname) != 0 {
			addresses = append(addresses, gatewayv1.GatewayStatusAddress{
				Type:  GatewayAddressTypePtr(gatewayv1.HostnameAddressType),
				Value: s.Hostname,
			})
		}
	}

	gw.Status.Addresses = addresses
	return nil
}

func (r *gatewayReconciler) filterHTTPRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1.HTTPRoute) []gatewayv1.HTTPRoute {
	var filtered []gatewayv1.HTTPRoute
	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) && isAllowed(ctx, r.Client, gw, &route) && len(computeHosts(gw, route.Spec.Hostnames)) > 0 {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (r *gatewayReconciler) filterTLSRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1.TLSRoute) []gatewayv1.TLSRoute {
	var filtered []gatewayv1.TLSRoute
	for _, route := range routes {
		if isAttachable(ctx, gw, &route, route.Status.Parents) && isAllowed(ctx, r.Client, gw, &route) && len(computeHosts(gw, route.Spec.Hostnames)) > 0 {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func (r *gatewayReconciler) updateStatus(ctx context.Context, original, modified *gatewayv1.Gateway) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := modified.Status.DeepCopy()

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
		return nil
	}
	return r.Client.Status().Update(ctx, modified)
}

func (r *gatewayReconciler) handleReconcileErrorWithStatus(ctx context.Context, reconcileErr error, original, modified *gatewayv1.Gateway) (ctrl.Result, error) {
	err := r.updateStatus(ctx, original, modified)
	if err != nil {
		return controllerruntime.Fail(fmt.Errorf("failed to update Gateway status while handling the reconcile error: %w: %w", reconcileErr, err))
	}
	return controllerruntime.Fail(reconcileErr)
}

func isAttachable(_ context.Context, gw *gatewayv1.Gateway, route metav1.Object, parents []gatewayv1.RouteParentStatus) bool {
	for _, rps := range parents {
		if helpers.NamespaceDerefOr(rps.ParentRef.Namespace, route.GetNamespace()) != gw.GetNamespace() ||
			string(rps.ParentRef.Name) != gw.GetName() {
			continue
		}

		for _, cond := range rps.Conditions {
			if cond.Type == string(gatewayv1.RouteConditionAccepted) && cond.Status == metav1.ConditionTrue {
				return true
			}

			if cond.Type == string(gatewayv1.RouteConditionResolvedRefs) && cond.Status == metav1.ConditionFalse {
				return true
			}
		}
	}
	return false
}

func (r *gatewayReconciler) setBackendTLSPolicyStatuses(scopedLog *slog.Logger,
	ctx context.Context,
	httpRoutes []gatewayv1.HTTPRoute,
	btlspMap helpers.BackendTLSPolicyServiceMap,
	gatewayName types.NamespacedName,
) error {
	scopedLog = r.logger
	r.logger.Debug("Updating BackendTLSPolicy statuses for Gateway %v ", btlspMap)

	currentGatewayRef := gatewayv1.ParentReference{
		Group:     ptr.To[gatewayv1.Group]("gateway.networking.k8s.io"),
		Kind:      ptr.To[gatewayv1.Kind]("Gateway"),
		Namespace: (*gatewayv1.Namespace)(&gatewayName.Namespace),
		Name:      gatewayv1.ObjectName(gatewayName.Name),
	}

	// This is then used both as a flag to see if other targetRefs in the same
	// Policy should create status updates or not.
	confirmedValidBTLSPs := make(map[types.NamespacedName]struct{})

	// svcNames have already had the conflict-resolution rules applied to build the btlspMap.
	// So, we can rely both on them being correct, and being referenced in the BackendTLSPolicy.
	// For each svcName, check if that service rolls up to a relevant Gateway
	// and run any required Policy checks, like if the Service exists.
	for svcName, collection := range btlspMap {

		// First, we get all the HTTPRoutes that have the targetRef service as a backend
		hrList := &gatewayv1.HTTPRouteList{}

		// if err := r.Client.List(ctx, hrList, &client.ListOptions{
		// 	FieldSelector: fields.OneTermEqualSelector(indexers.BackendServiceHTTPRouteIndex, svcName.String()),
		// }); err != nil {
		if err := r.Client.List(ctx, hrList); err != nil {
			// when the backendtlspolicy not exists yet
			scopedLog.ErrorContext(ctx, "Failed to get related HTTPRoutes", logfields.Error, err)
			return err
		}

		found, err := helpers.ContainsCommonHTTPRoute(hrList.Items, httpRoutes)
		if err != nil {
			// There was a common HTTPRoute found, but the generation was different, error out from this.
			scopedLog.ErrorContext(ctx, "Different generation comparing a HTTPRoute, re-reconciling", logfields.Error, err)
			return err
		}
		// If the index did not find any routes, also check httpRoutes directly for ext_authz
		// filter backends. The BackendServiceHTTPRouteIndex only covers backendRefs; ext_authz
		// backends are added by a separate indexer fix that may not yet have re-indexed
		// routes that existed before the operator started.
		if !found {
			for _, hr := range httpRoutes {
				for _, rule := range hr.Spec.Rules {
					for _, f := range rule.Filters {
						if f.Type != gatewayv1.HTTPRouteFilterExternalAuth || f.ExternalAuth == nil {
							continue
						}
						ns := helpers.NamespaceDerefOr(f.ExternalAuth.BackendRef.Namespace, hr.Namespace)
						if string(f.ExternalAuth.BackendRef.Name) == svcName.Name && ns == svcName.Namespace {
							found = true
						}
					}
				}
			}
		}
		if !found {
			continue
		}

		obj := &corev1.Service{}
		err = r.Client.Get(ctx, svcName, obj)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				// if it is not just a not found error, we should return the error as something is bad
				return fmt.Errorf("error while checking Backend Service: %w", err)
			}
			// If the Service does not exist, all referenced BackendTLSPolicies must be
			// Accepted: False, with reason Conflicted.
			for _, original := range collection.Valid {
				btlspFullName := client.ObjectKeyFromObject(original)

				if _, ok := confirmedValidBTLSPs[btlspFullName]; ok {
					// If the BackendTLSPolicy is already listed in the btlspStatus,
					// then we've already confirmed it's valid, so we need to skip updating
					// the status with errors.
					continue
				}

				btlsp := original.DeepCopy()

				input := &policychecks.BackendTLSPolicyInput{
					Client:           r.Client,
					APIReader:        r.APIReader,
					BackendTLSPolicy: btlsp,
					ControllerName:   "io.dolphin/gateway-controller",
				}
				input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
					Type:    string(gatewayv1.PolicyConditionAccepted),
					Status:  metav1.ConditionFalse,
					Reason:  string(gatewayv1.PolicyReasonInvalid),
					Message: fmt.Sprintf("TargetRef does not exist: %s", svcName),
				})
				input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
					Type:    string(gatewayv1.RouteConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(gatewayv1.RouteReasonBackendNotFound),
					Message: fmt.Sprintf("TargetRef does not exist: %s", svcName),
				})
				// Checks finished, apply the status to the actual objects.
				if err := r.updateBackendTLSPolicyStatus(ctx, scopedLog, original, btlsp); err != nil {
					return fmt.Errorf("failed to update BackendTLSPolicy status: %w", err)
				}
				// Update the original with the updated status
				original.Status = btlsp.Status
			}

			// Second, for any Conflicted BackendTLSPolicies, we can set them to Conflicted and move on.
			for _, original := range collection.Conflicted {
				btlspFullName := types.NamespacedName{
					Name:      original.GetName(),
					Namespace: original.GetNamespace(),
				}

				btlsp := original.DeepCopy()

				if _, ok := confirmedValidBTLSPs[btlspFullName]; ok {
					continue
				}
				input := &policychecks.BackendTLSPolicyInput{
					Client:           r.Client,
					APIReader:        r.APIReader,
					BackendTLSPolicy: btlsp,
					ControllerName:   "io.dolphin/gateway-controller",
				}
				input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
					Type:    string(gatewayv1.PolicyConditionAccepted),
					Status:  metav1.ConditionFalse,
					Reason:  string(gatewayv1.PolicyReasonInvalid),
					Message: fmt.Sprintf("TargetRef does not exist: %s", svcName),
				})
				input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
					Type:    string(gatewayv1.RouteConditionResolvedRefs),
					Status:  metav1.ConditionFalse,
					Reason:  string(gatewayv1.RouteReasonBackendNotFound),
					Message: fmt.Sprintf("TargetRef does not exist: %s", svcName),
				})
				// Checks finished, apply the status to the actual objects.
				if err := r.updateBackendTLSPolicyStatus(ctx, scopedLog, original, btlsp); err != nil {
					return fmt.Errorf("failed to update BackendTLSPolicy status: %w", err)
				}
				// Update the original with the updated status
				original.Status = btlsp.Status
			}
			// Continue, because this Service doesn't exist
			continue
		}

		for sectionName, original := range collection.Valid {

			btlsp := original.DeepCopy()

			inputLogger := scopedLog.With("BackendTLSPolicy", client.ObjectKeyFromObject(btlsp))
			// input for the validators
			// The validators will mutate the BackendTLSPolicy as required, setting its status correctly.
			input := &policychecks.BackendTLSPolicyInput{
				Client:           r.Client,
				BackendTLSPolicy: btlsp,
				APIReader:        r.APIReader,
				ControllerName:   "io.dolphin/gateway-controller",
			}

			// set Accepted to okay, this will be overwritten in checks if needed
			input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
				Type:    string(gatewayv1.PolicyConditionAccepted),
				Status:  metav1.ConditionTrue,
				Reason:  string(gatewayv1.PolicyReasonAccepted),
				Message: "Accepted BackendTLSPolicy",
			})

			// set ResolvedRefs to okay, this wil be overwritten in checks if needed
			input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
				Type:    string(gatewayv1.RouteConditionResolvedRefs),
				Status:  metav1.ConditionTrue,
				Reason:  string(gatewayv1.RouteReasonResolvedRefs),
				Message: "All references are valid",
			})
			inputLogger.Debug("Validating BackendTLSPolicy spec")
			valid, err := input.ValidateSpec(ctx, inputLogger, currentGatewayRef)
			if err != nil {
				return fmt.Errorf("failed to validate BackendTLSPolicy spec: %w", err)
			}
			if valid {
				// This BackendTLSPolicy is valid, so we can add the original status to the btlspStatus
				// lookup map. It's okay to do this multiple times, since the original status will be the same.
				confirmedValidBTLSPs[types.NamespacedName{
					Name:      btlsp.GetName(),
					Namespace: btlsp.GetNamespace(),
				}] = struct{}{}
			} else {
				// This BackendTLSPolicy is invalid, so it should be removed from the valid
				// map and added to the invalid map to ensure it's not used by the
				// ingestion logic.
				collection.DeleteValidPolicy(sectionName)
				collection.UpsertInvalidPolicy(sectionName, original)
			}

			// Checks finished, apply the status to the actual objects.
			if err := r.updateBackendTLSPolicyStatus(ctx, scopedLog, original, btlsp); err != nil {
				return fmt.Errorf("failed to update BackendTLSPolicy status: %w", err)
			}
			// Update the original with the updated status
			original.Status = btlsp.Status
		}

		// We can set Conflicted BTLSPs conditions now.
		for _, original := range collection.Conflicted {
			btlsp := original.DeepCopy()

			// input for the validators
			// The validators will mutate the BackendTLSPolicy as required, setting its status correctly.
			input := &policychecks.BackendTLSPolicyInput{
				Client:           r.Client,
				BackendTLSPolicy: btlsp,
				ControllerName:   "io.dolphin/gateway-controller",
			}

			input.SetAncestorCondition(currentGatewayRef, metav1.Condition{
				Type:    string(gatewayv1.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.PolicyReasonConflicted),
				Message: "BackendTLSPolicy conflicts with another",
			})
			// Checks finished, apply the status to the actual objects.
			if err := r.updateBackendTLSPolicyStatus(ctx, scopedLog, original, btlsp); err != nil {
				return fmt.Errorf("failed to update BackendTLSPolicy status: %w", err)
			}
			// Update the original with the updated status
			original.Status = btlsp.Status

		}
	}
	return nil
}

func (r *gatewayReconciler) updateBackendTLSPolicyStatus(ctx context.Context, scopedLog *slog.Logger, original *gatewayv1.BackendTLSPolicy, new *gatewayv1.BackendTLSPolicy) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, lastTransitionTime)) {
		return nil
	}
	scopedLog.Debug("BackendTLSPolicy status", types.NamespacedName{Name: original.Name, Namespace: original.Namespace})
	return r.Client.Status().Update(ctx, new)
}
