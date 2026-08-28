package gateway_api

import (
	"context"
	"encoding/pem"

	gatewayapihelpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	// myself
)

const (
	kindGateway   = "Gateway"
	kindHTTPRoute = "HTTPRoute"
	kindTLSRoute  = "TLSRoute"
	kindUDPRoute  = "UDPRoute"
	kindTCPRoute  = "TCPRoute"
	kindService   = "Service"
	kindSecret    = "Secret"
)

func GatewayAddressTypePtr(addr gatewayv1.AddressType) *gatewayv1.AddressType {
	return &addr
}

func setMergedLabelsAndAnnotations(temp, desired client.Object) {
	temp.SetAnnotations(mergeMap(temp.GetAnnotations(), desired.GetAnnotations()))
	temp.SetLabels(mergeMap(temp.GetLabels(), desired.GetLabels()))
}

// listenerisAllowed reports whether route may attach to listener.
func listenerisAllowed(gw *gatewayv1.Gateway, listener *gatewayv1.Listener, route metav1.Object, namespaceLabels gatewayapihelpers.NamespaceLabelIndex) bool {
	if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil {
		return gatewayapihelpers.IsListenerNamespaceAllowed(*listener, route.GetNamespace(), gw.GetNamespace(), namespaceLabels)
	}

	// check if route is kind-allowed
	if !isKindAllowed(*listener, route) {
		return false
	}
	return gatewayapihelpers.IsListenerNamespaceAllowed(*listener, route.GetNamespace(), gw.GetNamespace(), namespaceLabels)
}

func mergeMap(left, right map[string]string) map[string]string {
	if left == nil {
		return right
	}
	for key, value := range right {
		left[key] = value
	}
	return left
}

func GroupPtr(name string) *gatewayv1.Group {
	grp := gatewayv1.Group(name)
	return &grp
}

func getSupportedGroupKind(protocol gatewayv1.ProtocolType) (*gatewayv1.Group, gatewayv1.Kind) {
	switch protocol {
	case gatewayv1.HTTPProtocolType:
		return GroupPtr(gatewayv1.GroupName), kindHTTPRoute
	case gatewayv1.TLSProtocolType:
		return GroupPtr(gatewayv1alpha2.GroupName), kindTLSRoute
	case gatewayv1.HTTPSProtocolType:
		return GroupPtr(gatewayv1.GroupName), kindHTTPRoute
	case gatewayv1.TCPProtocolType:
		return GroupPtr(gatewayv1alpha2.GroupName), kindTCPRoute
	case gatewayv1.UDPProtocolType:
		return GroupPtr(gatewayv1alpha2.GroupName), kindUDPRoute
	default:
		return GroupPtr("Unknown"), "unkown"
	}
}

func groupDerefOr(group *gatewayv1.Group, defaultGroup string) string {
	if group != nil || *group != "" {
		return string(*group)
	}
	return ""
}

/*
this generic check could be more specific: public key, private key, or someother business logic
*/
func isValidPemFormat(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	p, rest := pem.Decode(b)
	if p == nil {
		return false
	}
	if len(rest) == 0 {
		return true
	}
	return isValidPemFormat(rest)
}

func isKindAllowed(listener gatewayv1.Listener, route metav1.Object) bool {
	if listener.AllowedRoutes.Kinds == nil {
		return true
	}
	routedKind := getGatewayKindForObject(route)

	for _, kind := range listener.AllowedRoutes.Kinds {
		if kind.Group == nil || (string(*kind.Group) == gatewayv1.GroupName && kind.Kind == kindHTTPRoute && routedKind == kindHTTPRoute) {
			return true
		}
		if kind.Group == nil || (string(*kind.Group) == gatewayv1alpha2.GroupName && kind.Kind == kindTLSRoute && routedKind == kindTLSRoute) {
			return true
		}
	}
	return false
}

// generic
func computeHostsForListener[T ~string](listener *gatewayv1.Listener, hostnames []T) []string {
	return model.ComputeHosts(toStringSlice(hostnames), (*string)(listener.Hostname))
}

func toStringSlice[T ~string](s []T) []string {
	res := make([]string, 0, len(s))
	for _, astr := range s {
		res = append(res, string(astr))
	}
	return res
}

func computeHosts[T ~string](gw *gatewayv1.Gateway, hostnames []T) []string {
	hosts := make([]string, 0, len(hostnames))
	for _, listener := range gw.Spec.Listeners {
		hosts = append(hosts, computeHostsForListener(&listener, hostnames)...)
	}

	return hosts
}
func getGatewayKindForObject(obj metav1.Object) gatewayv1.Kind {
	switch obj.(type) {
	case *gatewayv1.HTTPRoute:
		return kindHTTPRoute
	case *gatewayv1alpha2.TLSRoute:
		return kindTLSRoute
	case *gatewayv1alpha2.TCPRoute:
		return kindTCPRoute
	case *gatewayv1alpha2.UDPRoute:
		return kindUDPRoute
	default:
		return "unknown"
	}
}

func getAllDolphinGatewaysSet(ctx context.Context, c client.Client) (map[string]struct{}, error) {
	// Fetch all the Dolphin-relevant Gateways using the indexers.ImplementationGatewayIndex.
	gwList := &gatewayv1.GatewayList{}
	if err := c.List(ctx, gwList); err != nil {
		return nil, err
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

	return allDolphinGatewaysSet, nil
}
