package annotations

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
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

	RequestTimeoutAnnotation   = IngressPrefix + "/request-timeout"
	ForceHTTPSAnnotation       = IngressPrefix + "/force-https"
	HostListenerPortAnnotation = IngressPrefix + "/host-listener-port"

	enabled  = "enabled"
	disabled = "disabled"
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

// GetAnnotationRequestTimeout retrieves the RequestTimeout annotation's value.
func GetAnnotationRequestTimeout(ingress *networkingv1.Ingress) (*time.Duration, error) {
	val, exists := annotation.Get(ingress, RequestTimeoutAnnotation)
	if !exists {
		return nil, nil
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration %q: %w", val, err)
	}

	return &d, nil
}

func GetAnnotationForceHTTPSEnabled(ingress *networkingv1.Ingress) *bool {
	val, exists := annotation.Get(ingress, ForceHTTPSAnnotation)
	if !exists {
		return nil
	}

	if val == enabled {
		return model.AddressOf(true)
	}

	if val == disabled {
		return model.AddressOf(false)
	}

	boolVal, err := strconv.ParseBool(val)
	if err != nil {
		return nil
	}

	if boolVal {
		return model.AddressOf(true)
	}

	return model.AddressOf(false)
}

// GetAnnotationHostListenerPort returns the host listener port for the ingress if possible.
func GetAnnotationHostListenerPort(ingress *networkingv1.Ingress) (*uint32, error) {
	val, exists := annotation.Get(ingress, HostListenerPortAnnotation)
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
