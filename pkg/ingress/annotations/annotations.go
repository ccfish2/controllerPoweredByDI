package annotations

import (
	"strconv"

	"github.com/ccfish2/infra/pkg/annotation"
	networkingv1 "k8s.io/api/networking/v1"
)

const (
	Prefix        = "dolphin.io"
	IngressPrefix = "ingress.dolphin.io"

	LBModeAnnotation      = IngressPrefix + "/loadbalancer-mode"
	LBModeAnnotationAlias = Prefix + ".ingress" + "/loadbalancer-mode"

	TLSPassthroughAnnotation      = IngressPrefix + "/tls-passthrough"
	TLSPassthroughAnnotationAlias = Prefix + ".ingress" + "/tls-passthrough"

	enabled = "enabled"
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
