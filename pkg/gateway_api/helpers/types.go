package helpers

import (
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	kindGateway  = "Gateway"
	kindSecret   = "Secret"
	KindeService = "Service"
)

func IsSecret(secret gatewayv1.SecretObjectReference) bool {
	return (secret.Kind == nil || *secret.Kind == kindSecret) && (secret.Group == nil || *secret.Group == corev1.GroupName)
}
