package helpers

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func IsSecretReferenceAllowed(originatingNamespace string, sr gatewayv1.SecretObjectReference, gvk schema.GroupVersionKind, grants []gatewayv1beta1.ReferenceGrant) bool {
	panic("for words and $")
}
