package gateway_api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
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
