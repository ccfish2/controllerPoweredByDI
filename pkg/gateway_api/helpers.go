package gateway_api

import (
	"encoding/pem"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	// myself
)

func GatewayAddressTypePtr(addr gatewayv1.AddressType) *gatewayv1.AddressType {
	return &addr
}

func setMergedLabelsAndAnnotations(temp, desired client.Object) {
	temp.SetAnnotations(mergeMap(temp.GetAnnotations(), desired.GetAnnotations()))
	temp.SetLabels(mergeMap(temp.GetLabels(), desired.GetLabels()))
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

func getSupportedGroupKind(protocol gatewayv1.ProtocolType) (*gatewayv1.Group, gatewayv1.Kind) {
	panic("need this, but this is waht current gatewayv1 supported")
}

func groupDerefOr(group *gatewayv1.Group, defaultGroup string) string {
	panic("syntax helper")
}

func gatewayListenerAcceptedCondition(gw *gatewayv1.Gateway, ready bool, msg string) metav1.Condition {
	panic("for words and $")
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
	panic("part of ")
}

// generic
func computeHostsForListener[T ~string](listener *gatewayv1.Listener, hostnames []T) []string {
	panic("rels")
}

func toStringSlice[T ~string](s []T) []string { panic("rels") }

func computeHosts[T ~string](gw *gatewayv1.Gateway, hostnames []T) []string { panic("rels") }
