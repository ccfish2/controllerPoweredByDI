package gateway_api

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serros "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	//myself
	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/ingestion"
	translation "github.com/ccfish2/controllerPoweredByDI/pkg/model/translation/gateway-api"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
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
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failure")
	}

	// ignore deleting gateway
	if gw.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	copy := gw.DeepCopy()

	// step 2: Gather all required information for the ingestion model
	gwc := &gatewayv1.GatewayClass{}
	err = r.Client.Get(ctx,
		client.ObjectKey{Name: string(copy.Spec.GatewayClassName)}, gwc)
	if err != nil {
		scopedLog.WithField(gatewayClass, gw.Spec.GatewayClassName).
			WithError(err).
			Error("Unable to get GatewayClass")
		return controllerruntime.Success()
	}

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

	tlsRouteList := &gatewayv1alpha2.TLSRouteList{}
	err = r.Client.List(ctx, tlsRouteList)
	if err != nil {
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	servicesList := &corev1.ServiceList{}
	if err := r.Client.List(ctx, servicesList); err != nil {
		scopedLog.WithError(err).Error("Unable to list Services")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}

	httpListeners, tlsListeners := ingestion.GatewayAPI(ingestion.Input{
		GatewayClass: *gwc,
		Gateway:      *copy,
		HTTPRoutes:   r.filterHTTPRoutesByGateway(ctx, copy, httpRouteList.Items),
		TLSRoutes:    r.filterTLSRoutesByGateway(ctx, copy, tlsRouteList.Items),
		Services:     servicesList.Items,
	})

	err = r.setListenerStatus(ctx, copy, httpRouteList, tlsRouteList)
	if err != nil {
		scopedLog.WithError(err).Error("Unable to set listener status")
		setGatewayAccepted(copy, false, "Unable to set listener status")
		return r.handleReconcileErrorWithStatus(ctx, err, gw, copy)
	}
	setGatewayAccepted(copy, true, "Gateway successfully scheduled")

	// step 3: translate the listeners into dolphin model
	trans := translation.NewTranslator(r.SecretNamespace, r.IdleTimeoutSeconds)
	dec, svc, ep, err := trans.Translate(&model.Model{HTTP: httpListeners, TLS: tlsListeners})
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

// audit gateway routes configuration
// calculate and update the statistics into the GW status
// update collecting listeners info from gateway and update the total routes
func (r *gatewayReconciler) setListenerStatus(ctx context.Context, gw *gatewayv1.Gateway, httpRoutes *gatewayv1.HTTPRouteList, tlsRoutes *gatewayv1alpha2.TLSRouteList) error {
	grants := &gatewayv1beta1.ReferenceGrantList{}
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
func (r *gatewayReconciler) filterTLSRoutesByListener(ctx context.Context, gw *gatewayv1.Gateway, listener *gatewayv1.Listener, routes []gatewayv1alpha2.TLSRoute) []gatewayv1alpha2.TLSRoute {
	var filtered []gatewayv1alpha2.TLSRoute
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

func parentRefMatched(gw *gatewayv1.Gateway, lis *gatewayv1.Listener, namespace string, parefs []gatewayv1.ParentReference) bool {
	for _, ref := range refs {
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

// this enables running locally: either ingress or hostip would work on gateway-api
func (r *gatewayReconciler) setAddressStatus(ctx context.Context, gw *gatewayv1.Gateway) error {
	svcList := corev1.ServiceList{}
	if err := r.Client.List(ctx, &svcList, client.MatchingLabels{}, client.InNamespace(gw.Namespace)); err != nil {
		return fmt.Errorf("not found")
	}
	if len(svcList.Items) == 0 {
		return fmt.Errorf("")
	}

	addr := []gatewayv1.GatewayStatusAddress{}
	for _, svc := range svcList.Items {
		if len(svc.Status.LoadBalancer.Ingress) == 0 {
			continue
		}

		for _, ingr := range svc.Status.LoadBalancer.Ingress {
			addr = append(addr, gatewayv1.GatewayStatusAddress{
				// polymophism
				Type:  GatewayAddressTypePtr(gatewayv1.IPAddressType),
				Value: ingr.IP,
			})

			if ingr.Hostname != "" {
				addr = append(addr, gatewayv1.GatewayStatusAddress{
					Type:  GatewayAddressTypePtr(gatewayv1.HostnameAddressType),
					Value: ingr.IP,
				})
			}
		}

	}
	gw.Status.Addresses = addr
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

func (r *gatewayReconciler) filterTLSRoutesByGateway(ctx context.Context, gw *gatewayv1.Gateway, routes []gatewayv1alpha2.TLSRoute) []gatewayv1alpha2.TLSRoute {
	var filtered []gatewayv1alpha2.TLSRoute
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
