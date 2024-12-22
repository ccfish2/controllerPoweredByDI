package annotations

import (
	networkingv1 "k8s.io/api/networking/v1"
)

// ingress use annotations configure options
func GetAnnotationIngressLoadbalancerMode(ingress *networkingv1.Ingress) string {
	// use annotation package utility
	return ""
}
