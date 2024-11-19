package ingestion

import (
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
)

const (
	allHosts = "*"
)

// Input is the input for GatewayAPI.
type Input struct {
	GatewayClass    gatewayv1.GatewayClass
	Gateway         gatewayv1.Gateway
	HTTPRoutes      []gatewayv1.HTTPRoute
	TLSRoutes       []gatewayv1alpha2.TLSRoute
	GRPCRoutes      []gatewayv1alpha2.GRPCRoute
	ReferenceGrants []gatewayv1beta1.ReferenceGrant
	Services        []corev1.Service
}

// translate gateway resources into a model
func GatewayAPI(input Input) ([]model.HTTPListener, []model.TLSListener) {
	var resHTTP []model.HTTPListener
	var resTLS []model.TLSListener

	var labels, annotations map[string]string
	if input.Gateway.Spec.Infrastructure != nil {
		labels = toMapLabelString(input.Gateway.Spec.Infrastructure.Labels)
		annotations = toMapString(input.Gateway.Spec.Infrastructure.Annotations)
	}

	var infra *model.Infrastructure
	if labels != nil || annotations != nil {
		infra = &model.Infrastructure{
			Labels:      labels,
			Annotations: annotations,
		}
	}

	for _, l := range input.Gateway.Spec.Listeners {
		if l.Protocol != gatewayv1.HTTPProtocolType &&
			l.Protocol != gatewayv1.HTTPSProtocolType &&
			l.Protocol != gatewayv1.TLSProtocolType {
			continue
		}

		var httpRoutes []model.HTTPRoute
		httpRoutes = append(httpRoutes, toHTTPRoutes(l, input.HTTPRoutes, input.Services, input.ReferenceGrants)...)
		httpRoutes = append(httpRoutes, toGRPCRoutes(l, input.GRPCRoutes, input.Services, input.ReferenceGrants)...)
		resHTTP = append(resHTTP, model.HTTPListener{
			Name: string(l.Name),
			Sources: []model.FullyQualifiedResource{
				{
					Name:      input.Gateway.GetName(),
					Namespace: input.Gateway.GetNamespace(),
					Group:     input.Gateway.GroupVersionKind().Group,
					Version:   input.Gateway.GroupVersionKind().Version,
					Kind:      input.Gateway.GroupVersionKind().Kind,
					UID:       string(input.Gateway.GetUID()),
				},
			},
			Port:           uint32(l.Port),
			Hostname:       toHostname(l.Hostname),
			TLS:            toTLS(l.TLS, input.ReferenceGrants, input.Gateway.GetNamespace()),
			Routes:         httpRoutes,
			Infrastructure: infra,
		})

		resTLS = append(resTLS, model.TLSListener{
			Name: string(l.Name),
			Sources: []model.FullyQualifiedResource{
				{
					Name:      input.Gateway.GetName(),
					Namespace: input.Gateway.GetNamespace(),
					Group:     input.Gateway.GroupVersionKind().Group,
					Version:   input.Gateway.GroupVersionKind().Version,
					Kind:      input.Gateway.GroupVersionKind().Kind,
					UID:       string(input.Gateway.GetUID()),
				},
			},
			Port:           uint32(l.Port),
			Hostname:       toHostname(l.Hostname),
			Routes:         toTLSRoutes(l, input.TLSRoutes, input.Services, input.ReferenceGrants),
			Infrastructure: infra,
		})
	}

	return resHTTP, resTLS
}

func toGRPCRoutes(listener gatewayv1beta1.Listener, input []gatewayv1alpha2.GRPCRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.HTTPRoute {
	panic("steps needs to be baked")
}

func toTLSRoutes(listener gatewayv1beta1.Listener, input []gatewayv1alpha2.TLSRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.TLSRoute {
	panic("details is going on")
}

func toHTTPRoutes(listener gatewayv1.Listener, input []gatewayv1.HTTPRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.HTTPRoute {
	panic("")
}

func toTLS(tls *gatewayv1.GatewayTLSConfig, grants []gatewayv1beta1.ReferenceGrant, defaultNamespace string) []model.TLSSecret {
	panic("")
}

func toHostname(hostname *gatewayv1.Hostname) string {
	if hostname != nil {
		return (string)(*hostname)
	}
	return allHosts
}

func toMapString(in map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[string(k)] = string(v)
	}
	return out
}

func toMapLabelString(in map[gatewayv1.LabelKey]gatewayv1.LabelValue) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[string(k)] = string(v)
	}
	return out
}

func toStringSlice(s []gatewayv1.Hostname) []string {
	res := make([]string, 0, len(s))
	for _, h := range s {
		res = append(res, string(h))
	}
	return res
}
