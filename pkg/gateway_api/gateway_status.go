package gateway_api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func setGatewayAccepted(gw *gatewayv1.Gateway, accepted bool, msg string) gatewayv1.Gateway {
	panic("")
}

func setGatewayProgrammed(gw *gatewayv1.Gateway, ready bool, msg string) *gatewayv1.Gateway {
	panic("impl me")
}

func gatewayListenerInvalidRouteKinds(gw *gatewayv1.Gateway, msg string) metav1.Condition {
	panic("basic")
}

func gatewayListenerProgrammedCondition(gw *gatewayv1.Gateway, ready bool, msg string) metav1.Condition {
	panic("for words, not for up")
}
