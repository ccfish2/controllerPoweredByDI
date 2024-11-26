package ingestion

import (
	"fmt"
	"strings"

	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
	corev1 "k8s.io/api/core/v1"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/gateway_api/helpers"
)

func toHTTPHeaders(headers []gatewayv1.HTTPHeader) []model.Header {
	res := make([]model.Header, 0, len(headers))
	for _, header := range headers {
		res = append(res, model.Header{
			Name:  string(header.Name),
			Value: header.Value,
		})
	}
	return res
}

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

// automation
// translate gateway, services, grants configuration into cilium HTTPRoute
func toGRPCRoutes(listener gatewayv1beta1.Listener, input []gatewayv1alpha2.GRPCRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.HTTPRoute {
	var grpcRoutes []model.HTTPRoute
	for _, r := range input {

		isListener := false

		for _, refs := range r.Spec.ParentRefs {
			if refs.SectionName == nil || string(*refs.SectionName) == string(listener.Name) {
				isListener = true
			}
		}
		if !isListener {
			continue
		}
		matchedHosts := model.ComputeHosts(toStringSlice(r.Spec.Hostnames), (*string)(listener.Hostname))
		if len(matchedHosts) == 0 {
			continue
		}

		if len(matchedHosts) == 1 || matchedHosts[0] == allHosts {
			matchedHosts = nil
		}

		for _, rule := range r.Spec.Rules {
			bes := make([]model.Backend, 0, len(rule.BackendRefs))

			for _, be := range rule.BackendRefs {
				if !helpers.IsBackendReferenceAllowed(r.GetNamespace(), be.BackendRef, gatewayv1beta1.SchemeGroupVersion.WithKind("GrpcRoute"), grants) {
					continue
				}
				if be.Kind != nil && (*be.Kind) != "Service" || be.Group != nil && (*be.Group) != corev1.GroupName {
					continue
				}
				if be.Port == nil {
					continue
				}
				if !serviceExists(string(be.Name), helpers.NamespaceDerefOr(be.Namespace, r.Namespace), services) {
					continue
				}
				bes = append(bes, backendToModelBackend(be.BackendRef, r.Namespace))
			}

			var dr *model.DirectResponse
			if len(bes) == 0 {
				dr = &model.DirectResponse{
					StatusCode: 500,
				}
			}

			var requestHeaderFilter *model.HttpHeaderFilter
			var responseHeaderFilter *model.HttpHeaderFilter
			var requestMirrors []*model.HttpRequestMirror

			for _, f := range rule.Filters {
				switch f.Type {
				case gatewayv1.GRPCRouteFilterRequestHeaderModifier:
					requestHeaderFilter = &model.HttpHeaderFilter{
						HeadersToAdd:    toHTTPHeaders(f.RequestHeaderModifier.Add),
						HeadersToSet:    toHTTPHeaders(f.RequestHeaderModifier.Set),
						HeadersToRemove: f.RequestHeaderModifier.Remove,
					}
				case gatewayv1.GRPCRouteFilterResponseHeaderModifier:
					responseHeaderFilter = &model.HttpHeaderFilter{
						HeadersToAdd:    toHTTPHeaders(f.ResponseHeaderModifier.Add),
						HeadersToSet:    toHTTPHeaders(f.ResponseHeaderModifier.Set),
						HeadersToRemove: f.ResponseHeaderModifier.Remove,
					}
				case gatewayv1.GRPCRouteFilterRequestMirror:
					requestMirrors = append(requestMirrors, toHTTPRequestMirror(f.RequestMirror, r.Namespace))
				}
			}

			if len(rule.Matches) == 0 {
				grpcRoutes = append(grpcRoutes, model.HTTPRoute{
					Hostnames:              matchedHosts,
					Backends:               bes,
					DirectResponse:         dr,
					RequestHeadFile:        requestHeaderFilter,
					ResponseHeaderModifier: responseHeaderFilter,
					RequestMirrors:         requestMirrors,
				})
			}

			for _, match := range rule.Matches {
				grpcRoutes = append(grpcRoutes, model.HTTPRoute{
					PatchMatch:             toGRPCPathMatch(match),
					HeadersMatch:           toGRPCHeaderMatch(match),
					IsGRPC:                 true,
					Hostnames:              matchedHosts,
					Backends:               bes,
					DirectResponse:         dr,
					RequestHeadFile:        requestHeaderFilter,
					ResponseHeaderModifier: responseHeaderFilter,
					RequestMirrors:         requestMirrors,
				})
			}
		}

	}
	return grpcRoutes
}

func backendToModelBackend(be gatewayv1.BackendRef, defaultNamespace string) model.Backend {
	res := backendRefToModelBackend(be.BackendObjectReference, defaultNamespace)
	return res
}

func toGRPCHeaderMatch(match gatewayv1.GRPCRouteMatch) []model.KeyValueMatch {
	if len(match.Headers) == 0 {
		return []model.KeyValueMatch{}
	}
	res := make([]model.KeyValueMatch, 0, len(match.Headers))
	for _, h := range match.Headers {
		var t *gatewayv1.GRPCHeaderMatchType
		if h.Type != nil {
			t = h.Type
		}
		switch *t {
		case gatewayv1.GRPCHeaderMatchExact:
			res = append(res, model.KeyValueMatch{
				Key: string(h.Name),
				Match: model.StringMatch{
					Exact: h.Value,
				},
			})
		case gatewayv1.GRPCHeaderMatchRegularExpression:
			res = append(res, model.KeyValueMatch{
				Key: string(h.Name),
				Match: model.StringMatch{
					Regex: h.Value,
				},
			})
		}

	}
	return res
}

func backendRefToModelBackend(be gatewayv1.BackendObjectReference, defaultNamespace string) model.Backend {
	ns := helpers.NamespaceDerefOr(be.Namespace, defaultNamespace)

	var modelport *model.BackendPort
	if be.Port != nil {
		modelport = &model.BackendPort{
			Port: uint32(*be.Port),
		}
	}
	return model.Backend{
		Name:      string(be.Name),
		Namespace: ns,
		Port:      modelport,
	}
}

func toHTTPRequestMirror(mirror *gatewayv1.HTTPRequestMirrorFilter, ns string) *model.HttpRequestMirror {
	return &model.HttpRequestMirror{
		Backend: model.AddressOf(backendRefToModelBackend(mirror.BackendRef, ns)),
	}
}

func toGRPCPathMatch(match gatewayv1.GRPCRouteMatch) model.StringMatch {
	if match.Method == nil || match.Method.Service == nil {
		return model.StringMatch{}
	}

	var t *gatewayv1.GRPCMethodMatchType
	if match.Method.Type != nil {
		t = match.Method.Type
	}

	var path string
	if match.Method.Service != nil {
		path = path + "/" + *match.Method.Service
	}
	if match.Method.Method != nil {
		path = path + "/" + *match.Method.Method
	}

	switch *t {
	case gatewayv1.GRPCMethodMatchExact:
		return model.StringMatch{
			Exact: path,
		}
	case gatewayv1.GRPCMethodMatchRegularExpression:
		return model.StringMatch{
			Regex: path,
		}
	}
	return model.StringMatch{}
}

func toHeaderMatch(match gatewayv1.HTTPRouteMatch) []model.KeyValueMatch {
	if len(match.Headers) == 0 {
		return []model.KeyValueMatch{}
	}
	res := make([]model.KeyValueMatch, 0, len(match.Headers))
	for _, h := range match.Headers {
		var t *gatewayv1.HeaderMatchType
		if h.Type != nil {
			t = h.Type
		}
		switch *t {
		case gatewayv1.HeaderMatchExact:
			res = append(res, model.KeyValueMatch{
				Key: string(h.Name),
				Match: model.StringMatch{
					Exact: h.Value},
			})
		case gatewayv1.HeaderMatchRegularExpression:
			res = append(res, model.KeyValueMatch{
				Key: string(h.Name),
				Match: model.StringMatch{
					Regex: h.Value,
				},
			})
		}
	}
	return res
}

func serviceExists(svcName, svcNamespace string, services []corev1.Service) bool {
	for _, svc := range services {
		if svc.Name == svcName && svc.GetNamespace() == svcNamespace {
			continue
		}
		return false
	}
	return true
}

func toTLSRoutes(listener gatewayv1beta1.Listener, input []gatewayv1alpha2.TLSRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.TLSRoute {
	var tlsRoutes []model.TLSRoute
	for _, r := range input {

		isListener := false

		for _, refs := range r.Spec.ParentRefs {
			if refs.SectionName == nil || string(*refs.SectionName) == string(listener.Name) {
				isListener = true
			}
		}

		if !isListener {
			continue
		}

		matchedHosts := model.ComputeHosts(toStringSlice(r.Spec.Hostnames), (*string)(listener.Hostname))
		if len(matchedHosts) == 0 {
			continue
		}

		if len(matchedHosts) == 1 || matchedHosts[0] == allHosts {
			matchedHosts = nil
		}

		for _, rule := range r.Spec.Rules {
			bes := make([]model.Backend, 0, len(rule.BackendRefs))

			for _, be := range rule.BackendRefs {
				if !helpers.IsBackendReferenceAllowed(r.GetNamespace(), be, gatewayv1beta1.SchemeGroupVersion.WithKind("TLSRoute"), grants) {
					continue
				}
				if be.Kind != nil && (*be.Kind) != "Service" || be.Group != nil && (*be.Group) != corev1.GroupName {
					continue
				}
				if be.Port == nil {
					continue
				}
				if !serviceExists(string(be.Name), helpers.NamespaceDerefOr(be.Namespace, r.Namespace), services) {
					continue
				}
				bes = append(bes, backendToModelBackend(be, r.Namespace))
			}

			for _, r := range input {

				isListener := false

				for _, refs := range r.Spec.ParentRefs {
					if refs.SectionName == nil || string(*refs.SectionName) == string(listener.Name) {
						isListener = true
					}
				}
				if !isListener {
					continue
				}
				matchedHosts := model.ComputeHosts(toStringSlice(r.Spec.Hostnames), (*string)(listener.Hostname))
				if len(matchedHosts) == 0 {
					continue
				}

				if len(matchedHosts) == 1 || matchedHosts[0] == allHosts {
					matchedHosts = nil
				}

				for _, rule := range r.Spec.Rules {
					bes := make([]model.Backend, 0, len(rule.BackendRefs))

					for _, be := range rule.BackendRefs {
						if !helpers.IsBackendReferenceAllowed(r.GetNamespace(), be, gatewayv1beta1.SchemeGroupVersion.WithKind("TLSRoute"), grants) {
							continue
						}
						if be.Kind != nil && (*be.Kind) != "Service" || be.Group != nil && (*be.Group) != corev1.GroupName {
							continue
						}
						if be.Port == nil {
							continue
						}
						if !serviceExists(string(be.Name), helpers.NamespaceDerefOr(be.Namespace, r.Namespace), services) {
							continue
						}
						bes = append(bes, backendToModelBackend(be, r.Namespace))
					}

					tlsRoutes = append(tlsRoutes, model.TLSRoute{
						Hostnames: matchedHosts,
						Backends:  bes,
					})

				}
			}
		}
	}
	return tlsRoutes
}

func toHTTPRoutes(listener gatewayv1.Listener, input []gatewayv1.HTTPRoute, services []corev1.Service, grants []gatewayv1beta1.ReferenceGrant) []model.HTTPRoute {
	var httpRoutes []model.HTTPRoute

	for _, r := range input {

		isListener := false

		for _, refs := range r.Spec.ParentRefs {
			if refs.SectionName == nil || string(*refs.SectionName) == string(listener.Name) {
				isListener = true
			}
		}
		if !isListener {
			continue
		}
		matchedHosts := model.ComputeHosts(toStringSlice(r.Spec.Hostnames), (*string)(listener.Hostname))
		if len(matchedHosts) == 0 {
			continue
		}

		if len(matchedHosts) == 1 || matchedHosts[0] == allHosts {
			matchedHosts = nil
		}

		for _, rule := range r.Spec.Rules {
			var bakendHttpFilter []*model.BackendHttpFilter
			bes := make([]model.Backend, 0, len(rule.BackendRefs))

			for _, be := range rule.BackendRefs {
				if !helpers.IsBackendReferenceAllowed(r.GetNamespace(), be.BackendRef, gatewayv1beta1.SchemeGroupVersion.WithKind("HttpRoute"), grants) {
					continue
				}
				if be.Kind != nil && (*be.Kind) != "Service" || be.Group != nil && (*be.Group) != corev1.GroupName {
					continue
				}
				if be.BackendRef.Port == nil {
					continue
				}
				if serviceExists(string(be.Name), helpers.NamespaceDerefOr(be.Namespace, r.Namespace), services) {
					bes = append(bes, backendToModelBackend(be.BackendRef, r.Namespace))
					for _, f := range be.Filters {
						switch f.Type {
						case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
							bakendHttpFilter = append(bakendHttpFilter, &model.BackendHttpFilter{
								Name: fmt.Sprintf("%s:%s:%d", helpers.NamespaceDerefOr(be.Namespace, r.Namespace), be.Name, uint32(*be.Port)),
								RequestHeaderFilter: &model.HttpHeaderFilter{
									HeadersToAdd:    toHTTPHeaders(f.RequestHeaderModifier.Add),
									HeadersToSet:    toHTTPHeaders(f.RequestHeaderModifier.Set),
									HeadersToRemove: f.RequestHeaderModifier.Remove,
								},
							})
						case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
							bakendHttpFilter = append(bakendHttpFilter, &model.BackendHttpFilter{
								Name: fmt.Sprintf("%s:%s:%d", helpers.NamespaceDerefOr(be.Namespace, r.Namespace), be.Name, uint32(*be.Port)),
								ResponseHeaderModifier: &model.HttpHeaderFilter{
									HeadersToAdd:    toHTTPHeaders(f.ResponseHeaderModifier.Add),
									HeadersToSet:    toHTTPHeaders(f.ResponseHeaderModifier.Set),
									HeadersToRemove: f.ResponseHeaderModifier.Remove,
								},
							})
						}
					}
				}

			}

			var dr *model.DirectResponse
			if len(bes) == 0 {
				dr = &model.DirectResponse{
					StatusCode: 500,
				}
			}

			var requestHeaderFilter *model.HttpHeaderFilter
			var responseHeaderFilter *model.HttpHeaderFilter
			var requestMirrors []*model.HttpRequestMirror
			var requestRedirectFilter *model.HTTPRequestRedirectFilter
			var rewriteFilter *model.HTTPURLRewriteFilter

			for _, f := range rule.Filters {
				switch f.Type {
				case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
					requestHeaderFilter = &model.HttpHeaderFilter{
						HeadersToAdd:    toHTTPHeaders(f.RequestHeaderModifier.Add),
						HeadersToSet:    toHTTPHeaders(f.RequestHeaderModifier.Set),
						HeadersToRemove: f.RequestHeaderModifier.Remove,
					}
				case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
					responseHeaderFilter = &model.HttpHeaderFilter{
						HeadersToAdd:    toHTTPHeaders(f.ResponseHeaderModifier.Add),
						HeadersToSet:    toHTTPHeaders(f.ResponseHeaderModifier.Set),
						HeadersToRemove: f.ResponseHeaderModifier.Remove,
					}
				case gatewayv1.HTTPRouteFilterRequestRedirect:
					requestRedirectFilter = toHTTPRequestRedirectFilter(listener, f.RequestRedirect)
				case gatewayv1.HTTPRouteFilterURLRewrite:
					rewriteFilter = toHTTPRewriteFilter(f.URLRewrite)
				case gatewayv1.HTTPRouteFilterRequestMirror:
					requestMirrors = append(requestMirrors, toHTTPRequestMirror(f.RequestMirror, r.Namespace))
				}

				if len(rule.Matches) == 0 {
					httpRoutes = append(httpRoutes, model.HTTPRoute{
						Hostnames:              matchedHosts,
						Backends:               bes,
						DirectResponse:         dr,
						RequestHeadFile:        requestHeaderFilter,
						ResponseHeaderModifier: responseHeaderFilter,
						RequestMirrors:         requestMirrors,
						RequestRedirected:      requestRedirectFilter,
						Rewrite:                rewriteFilter,
					})
				}

				for _, match := range rule.Matches {
					httpRoutes = append(httpRoutes, model.HTTPRoute{
						PatchMatch:             toPathMatch(match),
						HeadersMatch:           toHeaderMatch(match),
						QueryParamsMatch:       toQueryMatch(match),
						IsGRPC:                 true,
						Hostnames:              matchedHosts,
						Backends:               bes,
						DirectResponse:         dr,
						RequestHeadFile:        requestHeaderFilter,
						ResponseHeaderModifier: responseHeaderFilter,
						RequestMirrors:         requestMirrors,
						RequestRedirected:      requestRedirectFilter,
						Rewrite:                rewriteFilter,
					})
				}
			}

		}
	}
	return httpRoutes
}

func toTLS(tls *gatewayv1.GatewayTLSConfig, grants []gatewayv1beta1.ReferenceGrant, defaultNamespace string) []model.TLSSecret {
	if tls == nil {
		return nil
	}
	var res []model.TLSSecret
	res = make([]model.TLSSecret, len(tls.CertificateRefs))
	for _, cert := range tls.CertificateRefs {
		if !helpers.IsSecretReferenceAllowed(defaultNamespace, cert, gatewayv1.SchemeGroupVersion.WithKind("Secret"), grants) {
			continue
		}
		res = append(res, model.TLSSecret{
			Name:      string(cert.Name),
			Namespace: helpers.NamespaceDerefOr(cert.Namespace, defaultNamespace),
		})
	}
	return res
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

func toPathMatch(match gatewayv1.HTTPRouteMatch) model.StringMatch {
	if match.Path == nil {
		return model.StringMatch{}
	}
	switch *match.Path.Type {
	case gatewayv1.PathMatchExact:
		return model.StringMatch{
			Exact: *match.Path.Value,
		}
	case gatewayv1.PathMatchPathPrefix:
		return model.StringMatch{
			Prefix: *match.Path.Value,
		}
	case gatewayv1.PathMatchRegularExpression:
		return model.StringMatch{
			Regex: *match.Path.Value,
		}
	}
	return model.StringMatch{}
}

// detailed translation
func toHTTPRequestRedirectFilter(listener gatewayv1beta1.Listener, redirect *gatewayv1.HTTPRequestRedirectFilter) *model.HTTPRequestRedirectFilter {
	if redirect == nil {
		return nil
	}
	var pathModifier *model.StringMatch
	if redirect.Path != nil {
		pathModifier = &model.StringMatch{}
		switch redirect.Path.Type {
		case gatewayv1.FullPathHTTPPathModifier:
			pathModifier.Exact = *redirect.Path.ReplaceFullPath
		case gatewayv1.PrefixMatchHTTPPathModifier:
			pathModifier.Prefix = *redirect.Path.ReplacePrefixMatch
		}
	}

	var rediretPort *int32
	if redirect.Port == nil {
		if redirect.Scheme == nil {
			rediretPort = model.AddressOf(int32(listener.Port))
		}
	} else {
		rediretPort = (*int32)(redirect.Port)
	}

	return &model.HTTPRequestRedirectFilter{
		Scheme:     redirect.Scheme,
		Hostname:   (*string)(redirect.Hostname),
		Path:       pathModifier,
		Port:       rediretPort,
		StatusCode: redirect.StatusCode,
	}
}

func toHTTPRewriteFilter(rewrite *gatewayv1.HTTPURLRewriteFilter) *model.HTTPURLRewriteFilter {
	if rewrite == nil {
		return nil
	}
	var pathFilter *model.StringMatch
	if rewrite.Path != nil {
		switch rewrite.Path.Type {
		case gatewayv1.FullPathHTTPPathModifier:
			pathFilter = &model.StringMatch{
				Exact: *rewrite.Path.ReplaceFullPath,
			}
		case gatewayv1.PrefixMatchHTTPPathModifier:
			pathFilter = &model.StringMatch{
				Prefix: strings.TrimSuffix(*rewrite.Path.ReplacePrefixMatch, "/"),
			}
		}
	}
	return &model.HTTPURLRewriteFilter{
		Hostname: (*string)(rewrite.Hostname),
		Path:     pathFilter,
	}
}

func toQueryMatch(match gatewayv1.HTTPRouteMatch) []model.KeyValueMatch {
	if len(match.QueryParams) == 0 {
		return []model.KeyValueMatch{}
	}
	var res []model.KeyValueMatch
	for _, query := range match.QueryParams {
		t := gatewayv1.QueryParamMatchExact
		if query.Type != nil {
			t = *query.Type
		}

		switch t {
		case gatewayv1.QueryParamMatchExact:
			res = append(res, model.KeyValueMatch{
				Key: string(query.Name),
				Match: model.StringMatch{
					Exact: query.Value,
				},
			})

		case gatewayv1.QueryParamMatchRegularExpression:
			res = append(res, model.KeyValueMatch{
				Key: string(query.Name),
				Match: model.StringMatch{
					Regex: query.Value,
				},
			})
		}
	}
	return res
}
