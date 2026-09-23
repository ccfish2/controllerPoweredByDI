package gateway_api

import (
	"context"
	"fmt"
	"log/slog"

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
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

func (r *gatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info("Reconciling Gateway")

	// step 1: retrieve the gateway
	gw := &gatewayv1.Gateway{}

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

	err = r.Client.Get(ctx,
		client.ObjectKey{Name: string(gw.Spec.GatewayClassName)},
		gwc,
	)

	if err != nil {
		scopedLog.WithFields(logrus.Fields{
			"gatewayClassName": gw.Spec.GatewayClassName,
			"error":            err,
		}).Error("Unable to get GatewayClass")

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

	httpRouteList := &gatewayv1.HTTPRouteList{}
	err = r.Client.List(ctx, httpRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	tlsRouteList := &gatewayv1.TLSRouteList{}
	err = r.Client.List(ctx, tlsRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	servicesList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, servicesList); err != nil {
		scopedLog.WithError(err).Error("Unable to list Services")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	grpcRouteList := &gatewayv1.GRPCRouteList{}
	if err := r.Client.List(ctx, grpcRouteList); err != nil {
		scopedLog.Error(ctx, "Unable to list GRPCRoutes", logfields.Error, err)
		return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
	}

	var namespaces []corev1.Namespace
	if hasAllowedRoutesNamespaceSelector(gw) {
		namespaceList := &corev1.NamespaceList{}
		if err := r.Client.List(ctx, namespaceList); err != nil {
			scopedLog.Error(ctx, "Unable to list Namespaces", logfields.Error, err)
			return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
		}
		namespaces = namespaceList.Items
	}
	namespaceLabels := helpers.NewNamespaceLabelIndex(namespaces)
	dgccfg := r.getGatewayClassConfig(ctx, gwc)
	HTTPRoutes := r.filterHTTPRoutesByGateway(ctx, copy, httpRouteList.Items)
	httpListeners, tlsListeners := ingestion.GatewayAPI(ingestion.Input{
		GatewayClass:       *gwc,
		Gateway:            *copy,
		GatewayClassConfig: dgccfg,
		HTTPRoutes:         HTTPRoutes,
		TLSRoutes:          r.filterTLSRoutesByGateway(ctx, copy, tlsRouteList.Items),
		GRPCRoutes:         r.filterGRPCRoutesByGateway(ctx, gw, grpcRouteList.Items, namespaceLabels),
		Services:           servicesList.Items,
	})

	btlspList := &gatewayv1.BackendTLSPolicyList{}
	if err := r.Client.List(ctx, btlspList); err != nil {
		scopedLog.WithError(err).Error("Unable to list BackendTLSPolicies")
		return r.handleReconcileErrorWithStatus(ctx, err, copy, gw)
	}
	if len(btlspList.Items) > 0 {
		btlspMap := helpers.BuildBackendTLSPolicyLookup(btlspList)
		if err := r.setBackendTLSPolicyStatuses(&slog.Logger{}, ctx, HTTPRoutes, btlspMap, req.NamespacedName); err != nil {
			scopedLog.WithError(err).Error("Unable to update BackendTLSPolicy Status")
			return controllerruntime.Fail(err)
		}
	}
	err = r.setListenerStatus(ctx, copy, httpRouteList, tlsRouteList, grpcRouteList, namespaceLabels)
	if err != nil {
		scopedLog.WithError(err).Error("Unable to set listener status")
		setGatewayAccepted(copy, false, "Unable to set listener status")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}
	setGatewayAccepted(copy, true, "Gateway successfully scheduled")

	// step 3: translate the listeners into dolphin model
	trans := translation.NewTranslator(r.SecretNamespace, r.IdleTimeoutSeconds, true, false)
	dec, svc, ep, err := trans.Translate(&model.Model{HTTP: httpListeners, TLS: tlsListeners}, dgccfg)
	if err != nil {
		scopedLog.WithError(err).Error("Unable to translate resources")
		setGatewayAccepted(gw, false, "Unable to translate resources")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	if err := r.ensureService(ctx, svc); err != nil {
		scopedLog.WithError(err).Error("Unable to create Service")
		setGatewayAccepted(gw, false, "Unable to create Service resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	if err := r.ensureEndpoints(ctx, ep); err != nil {
		scopedLog.WithError(err).Error("Unable to ensure Endpoints")
		setGatewayAccepted(gw, false, "Unable to ensure Endpoints resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	if err := r.ensureEnvoyConfig(ctx, dec); err != nil {
		scopedLog.WithError(err).Error("Unable to ensure DolphinEnvoyConfig")
		setGatewayAccepted(gw, false, "Unable to ensure CEC resource")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	// step 4: update the status of the gateway
	if err := r.setAddressStatus(ctx, copy); err != nil {
		scopedLog.WithError(err).Error("Address is not ready")
		setGatewayProgrammed(gw, false, "Address is not ready")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	setGatewayProgrammed(copy, true, "reconciled successfully")
	if err := r.updateStatus(ctx, gw, copy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update Gateway status: %w", err)
	}
	scopedLog.Info("Successfully reconciled Gateway")
	return reconcile.Result{}, nil
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

		var attachedRoutes int32
		attachedRoutes += int32(len(r.filterHTTPRoutesByListener(ctx, gw, &l, httpRoutes.Items)))
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
func (r *gatewayReconciler) filterHTTPRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routes []gatewayv1.HTTPRoute) []gatewayv1.HTTPRoute {
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
func (r *gatewayReconciler) filterTLSRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routes []gatewayv1.TLSRoute) []gatewayv1.TLSRoute {
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
