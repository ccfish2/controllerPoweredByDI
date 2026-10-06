package helpers

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// HasTCPRouteSupport returns if the TCPRoute CRD is supported.
// This checks if the Gateway API v1 TCPRoute CRD is registered in the client scheme.
func HasTCPRouteSupport(scheme *runtime.Scheme) bool {
	return scheme.Recognizes(GatewayV1GVK("TCPRoute"))
}

func IsParentAttachable(
	_ context.Context,
	reconcileParent metav1.Object,
	route metav1.Object,
	parents []gatewayv1.RouteParentStatus,
	attachedListenerSets []gatewayv1.ListenerSet,
) bool {
	return true
}
