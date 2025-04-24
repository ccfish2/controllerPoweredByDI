package annotations

import (
	"strconv"

	"github.com/ccfish2/infra/pkg/annotation"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

const (
	Prefix        = "dolphin.io"
	IngressPrefix = "ingress.dolphin.io"

	LBModeAnnotation      = IngressPrefix + "/loadbalancer-mode"
	LBModeAnnotationAlias = Prefix + ".ingress" + "/loadbalancer-mode"

	ServiceTypeAnnotation      = IngressPrefix + "/service-type"
	ServiceTypeAnnotationAlias = Prefix + ".ingress" + "/service-type"

	SecureNodePortAnnotation      = IngressPrefix + "/secure-node-port"
	SecureNodePortAnnotationAlias = Prefix + ".ingress" + "/secure-node-port"

	InsecureNodePortAnnotation      = IngressPrefix + "/insecure-node-port"
	InsecureNodePortAnnotationAlias = Prefix + ".ingress" + "/insecure-node-port"

	TLSPassthroughAnnotation      = IngressPrefix + "/tls-passthrough"
	TLSPassthroughAnnotationAlias = Prefix + ".ingress" + "/tls-passthrough"

	enabled = "enabled"
)

const (
	LoadbalancerModeDedicated = "dedicated"
	LoadbalancerModeShared    = "shared"
)

// ingress use annotations configure options
func GetAnnotationIngressLoadbalancerMode(ingress *networkingv1.Ingress) string {
	value, _ := annotation.Get(ingress, LBModeAnnotation, LBModeAnnotationAlias)
	return value
}

func GetAnnotationTLSPassthroughEnabled(ingress *networkingv1.Ingress) bool {
	val, exists := annotation.Get(ingress, TLSPassthroughAnnotation, TLSPassthroughAnnotationAlias)
	if !exists {
		return false
	}

	if val == enabled {
		return true
	}

	boolVal, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}

	return boolVal
}

func GetAnnotationServiceType(ingress *networkingv1.Ingress) string {
	val, exists := annotation.Get(ingress, ServiceTypeAnnotation, ServiceTypeAnnotationAlias)
	if !exists {
		return string(corev1.ServiceTypeLoadBalancer)
	}
	return val
}

// GetAnnotationSecureNodePort returns the secure node port for the ingress if possible.
func GetAnnotationSecureNodePort(ingress *networkingv1.Ingress) (*uint32, error) {
	val, exists := annotation.Get(ingress, SecureNodePortAnnotation, SecureNodePortAnnotationAlias)
	if !exists {
		return nil, nil
	}
	intVal, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return nil, err
	}
	res := uint32(intVal)
	return &res, nil
}

// GetAnnotationInsecureNodePort returns the insecure node port for the ingress if possible.
func GetAnnotationInsecureNodePort(ingress *networkingv1.Ingress) (*uint32, error) {
	val, exists := annotation.Get(ingress, InsecureNodePortAnnotation, InsecureNodePortAnnotationAlias)
	if !exists {
		return nil, nil
	}
	intVal, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return nil, err
	}
	res := uint32(intVal)
	return &res, nil
}
