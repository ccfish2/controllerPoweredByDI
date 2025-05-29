package routechecker

import (
	//myself
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func CheckGWAllowedFromNamespace(in Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := in.GetGateway()
	if err != nil {
		in.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "onvalid",
			Message: err.Error(),
		})
		return false, err
	}

	hasNamespaceRestriction := false
	for _, listener := range gw.Spec.Listeners {
		if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil {
			continue
		}
		if listener.Name != *parentRef.SectionName {
			continue
		}
		if listener.Hostname != nil && len(computeHostsForListener(&listener, in.GetHostnames())) > 0 {
			continue
		}

		if *listener.AllowedRoutes.Namespaces.From == gatewayv1.NamespacesFromAll {
			continue
		}

		if *listener.AllowedRoutes.Namespaces.From == gatewayv1.NamespacesFromSelector {
			nsList := corev1.NamespaceList{}
			selector, _ := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
			if err := in.GetClient().List(in.GetContext(), &nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
				in.SetParentCondition(metav1.Condition{
					Type:    "Accepted",
					Status:  metav1.ConditionFalse,
					Reason:  "could not retreive any namespace using the selector",
					Message: err.Error(),
				})
				return false, err
			}
			allowed := false
			for _, ns := range nsList.Items {
				if ns.Name == in.GetNamespace() {
					allowed = true
				}
			}
			if !allowed {
				in.SetParentCondition(metav1.Condition{
					Type:    "Accepted",
					Status:  metav1.ConditionFalse,
					Reason:  "no anmespaces selected is allowed by the gateway ",
					Message: err.Error(),
				})
				return false, nil
			}
			return true, nil
		}
		if *listener.AllowedRoutes.Namespaces.From == gatewayv1.NamespacesFromSame &&
			in.GetNamespace() == gw.GetNamespace() {
			return true, nil
		}
		hasNamespaceRestriction = true
	}
	if hasNamespaceRestriction {
		in.SetParentCondition(metav1.Condition{
			Type:    "Acccepted",
			Status:  metav1.ConditionFalse,
			Reason:  "Listeners namespaces are not allowed by the gateway",
			Message: "hasnamespace restriction",
		})
	}
	return true, nil
}

func computeHostsForListener(list *gatewayv1.Listener, hostnames []gatewayv1.Hostname) []string {
	return []string{}
}

func CheckGWMatchingPort(in Input, parentRef gatewayv1.ParentReference) (bool, error) {
	// get the gateway
	gw, err := in.GetGateway(parentRef)
	if err != nil {
		in.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionStatus(gatewayv1.GatewayConditionAccepted),
			Reason:  "parent does not match",
			Message: "failed to get hte gatewy from theintput",
		})
		return false, err
	}

	// find the matching port listener from the gateway
	if parentRef.Port != nil {

		for _, lis := range gw.Spec.Listeners {
			if lis.Port == *parentRef.Port {
				return true, nil
			}
		}

		in.SetParentCondition(parentRef, metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  "port does not match",
			Message: "non of the listener port matches the section port",
		})
		return false, err

	}
	return true, nil
}

func CheckGWMatchingSection(in Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := in.GetGateway(parentRef)
	if err != nil {
		in.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "onvalid",
			Message: "GVK kind is not the right one",
		})
		return false, err
	}
	if parentRef.SectionName != nil {
		found := false
		for _, lis := range gw.Spec.Listeners {
			if lis.Name == *parentRef.SectionName {
				found = true
				break
			}
		}
		if !found {
			in.SetParentCondition(parentRef, metav1.Condition{
				Type:    "Invlid",
				Status:  metav1.ConditionFalse,
				Reason:  "name does not match",
				Message: "section name does not match any listener name",
			})
			return false, err
		}
	}
	return true, nil
}

func CheckGatewayRouteKindAllowed(input Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := input.GetGateway(parentRef)
	if err != nil {
		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "Invalid" + input.GetGVK().Kind,
			Message: err.Error(),
		})

		return false, nil
	}

	for _, listener := range gw.Spec.Listeners {
		if listener.AllowedRoutes == nil || len(listener.AllowedRoutes.Kinds) == 0 {
			continue
		}

		allowed := false
		routeGVK := input.GetGVK()
		for _, kind := range listener.AllowedRoutes.Kinds {
			if (kind.Group == nil || (kind.Group != nil && *kind.Group == gatewayv1.Group(routeGVK.Group))) &&
				kind.Kind == gatewayv1.Kind(routeGVK.Kind) {
				allowed = true
				break
			}
		}

		if !allowed {
			input.SetParentCondition(parentRef, metav1.Condition{
				Type:    string(gatewayv1.RouteConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.RouteReasonNotAllowedByListeners),
				Message: routeGVK.Kind + " is not allowed to attach to this Gateway due to route kind restrictions",
			})

			return false, nil
		}
	}

	return true, nil
}

func CheckGatewayMatchingPorts(input Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := input.GetGateway(parentRef)
	if err != nil {
		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "Invalid" + input.GetGVK().Kind,
			Message: err.Error(),
		})

		return false, nil
	}

	if parentRef.Port != nil {
		for _, listener := range gw.Spec.Listeners {
			if listener.Port == *parentRef.Port {
				return true, nil
			}
		}
		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  "NoMatchingParent",
			Message: fmt.Sprintf("No matching listener with port %d", *parentRef.Port),
		})

		return false, nil
	}

	return true, nil
}

func CheckGatewayMatchingHostnames(input Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := input.GetGateway(parentRef)
	if err != nil {
		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "Invalid" + input.GetGVK().Kind,
			Message: err.Error(),
		})

		return false, nil
	}

	if len(computeHosts(gw, input.GetHostnames())) == 0 {

		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.RouteReasonNoMatchingListenerHostname),
			Message: "No matching listener hostname",
		})

		return false, nil
	}

	return true, nil
}

func CheckGatewayMatchingSection(input Input, parentRef gatewayv1.ParentReference) (bool, error) {
	gw, err := input.GetGateway(parentRef)
	if err != nil {
		input.SetParentCondition(parentRef, metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionFalse,
			Reason:  "Invalid" + input.GetGVK().Kind,
			Message: err.Error(),
		})

		return false, nil
	}

	if parentRef.SectionName != nil {
		found := false
		for _, listener := range gw.Spec.Listeners {
			if listener.Name == *parentRef.SectionName {
				found = true
				break
			}
		}
		if !found {
			input.SetParentCondition(parentRef, metav1.Condition{
				Type:    string(gatewayv1.RouteConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  "NoMatchingParent",
				Message: fmt.Sprintf("No matching listener with sectionName %s", *parentRef.SectionName),
			})

			return false, nil
		}
	}

	return true, nil
}
