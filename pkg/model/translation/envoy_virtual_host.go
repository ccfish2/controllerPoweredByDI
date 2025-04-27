package translation

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	envoy_config_core_v3 "github.com/cilium/proxy/go/envoy/config/core/v3"
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"
	envoy_type_matcher_v3 "github.com/cilium/proxy/go/envoy/type/matcher/v3"

	"github.com/ccfish2/infra/pkg/math"
)

const (
	wildCard       = "*"
	envoyAuthority = ":authority"
	slash          = "/"
	dot            = "."
	starDot        = "*."
	dotRegex       = "[.]"
	notDotRegex    = "[^.]"
)

type VirtualHostParameter struct {
	HostNames           []string
	HTTPSRedirect       bool
	HostNameSuffixMatch bool
	ListenerPort        uint32
}

type VirtualHostMutator func(*envoy_config_route_v3.VirtualHost) *envoy_config_route_v3.VirtualHost

func NewVirtualHostWithDefaults(httpRoutes []model.HTTPRoute, param VirtualHostParameter, mutators ...VirtualHostMutator) (*envoy_config_route_v3.VirtualHost, error) {
	return NewVirtualHost(httpRoutes, param, mutators...)
}

// route meats:  NewVirtualHost creates a new VirtualHost with the given host and routes.
func NewVirtualHost(httpRoutes []model.HTTPRoute, param VirtualHostParameter, mutators ...VirtualHostMutator) (*envoy_config_route_v3.VirtualHost, error) {
	var routes SortableRoute
	if param.HTTPSRedirect {
		routes = envoyHTTPSRoutes(httpRoutes, param.HostNames, param.HostNameSuffixMatch)
	} else {
		routes = envoyHTTPRoutes(httpRoutes, param.HostNames, param.HostNameSuffixMatch, param.ListenerPort)
	}

	// Related docs https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/route_matching
	sort.Stable(routes)

	var domains []string
	for _, host := range param.HostNames {
		if host == wildCard {
			domains = []string{wildCard}
			break
		}
		domains = append(domains,
			host,
			// match authority header with port (e.g. "example.com:80")
			net.JoinHostPort(host, wildCard),
		)
	}

	res := &envoy_config_route_v3.VirtualHost{
		Name:    domains[0],
		Domains: domains,
		Routes:  routes,
	}

	for _, fn := range mutators {
		res = fn(res)
	}

	return res, nil
}

type SortableRoute []*envoy_config_route_v3.Route

func (s SortableRoute) Len() int {
	return len(s)
}

func (s SortableRoute) Less(i, j int) bool {
	exactMatch1 := len(s[i].Match.GetPath())
	exactMatch2 := len(s[j].Match.GetPath())
	if exactMatch1 != exactMatch2 {
		return exactMatch1 > exactMatch2
	}

	regexMatch1 := len(s[i].Match.GetSafeRegex().GetRegex())
	regexMatch2 := len(s[j].Match.GetSafeRegex().GetRegex())
	if regexMatch1 != regexMatch2 {
		return regexMatch1 > regexMatch2
	}

	prefixMatch1 := math.IntMax(len(s[i].Match.GetPathSeparatedPrefix()), len(s[i].Match.GetPrefix()))
	prefixMatch2 := math.IntMax(len(s[j].Match.GetPathSeparatedPrefix()), len(s[j].Match.GetPrefix()))
	headerMatch1 := len(s[i].Match.GetHeaders())
	headerMatch2 := len(s[j].Match.GetHeaders())
	queryMatch1 := len(s[i].Match.GetQueryParameters())
	queryMatch2 := len(s[j].Match.GetQueryParameters())

	if prefixMatch1 != prefixMatch2 {
		return prefixMatch1 > prefixMatch2
	}

	if headerMatch1 != headerMatch2 {
		return headerMatch1 > headerMatch2
	}

	return queryMatch1 > queryMatch2
}

func (s SortableRoute) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func envoyHTTPSRoutes(httpRoutes []model.HTTPRoute, hostnames []string, hostNameSuffixMatch bool) []*envoy_config_route_v3.Route {
	matchBackendMap := make(map[string][]model.HTTPRoute)
	for _, r := range httpRoutes {
		matchBackendMap[r.GetMatchKey()] = append(matchBackendMap[r.GetMatchKey()], r)
	}

	routes := make([]*envoy_config_route_v3.Route, 0, len(matchBackendMap))
	for _, r := range httpRoutes {
		hRoutes, exists := matchBackendMap[r.GetMatchKey()]
		if !exists {
			continue
		}
		rRedirect := &envoy_config_route_v3.Route_Redirect{
			Redirect: &envoy_config_route_v3.RedirectAction{
				SchemeRewriteSpecifier: &envoy_config_route_v3.RedirectAction_HttpsRedirect{
					HttpsRedirect: true,
				},
			},
		}
		route := envoy_config_route_v3.Route{
			Match: getRouteMatch(hostnames,
				hostNameSuffixMatch,
				hRoutes[0].PathMatch,
				hRoutes[0].QueryParamsMatch,
				hRoutes[0].HeadersMatch,
				hRoutes[0].Method),
			Action: rRedirect,
		}
		routes = append(routes, &route)
		delete(matchBackendMap, r.GetMatchKey())
	}
	return routes
}

func envoyHTTPRoutes(httpRoutes []model.HTTPRoute, hostnames []string, hostNameSuffixMatch bool, listenerPort uint32) []*envoy_config_route_v3.Route {
	matchBackendMap := make(map[string][]model.HTTPRoute)
	for _, r := range httpRoutes {
		matchBackendMap[r.GetMatchKey()] = append(matchBackendMap[r.GetMatchKey()], r)
	}

	routes := make([]*envoy_config_route_v3.Route, 0, len(matchBackendMap))
	for _, r := range httpRoutes {
		hRoutes, exists := matchBackendMap[r.GetMatchKey()]
		if !exists {
			continue
		}
		var backends []model.Backend
		for _, r := range hRoutes {
			backends = append(backends, r.Backends...)
		}

		if len(backends) == 0 && hRoutes[0].RequestRedirect == nil {
			routes = append(routes, envoyHTTPRouteNoBackend(hRoutes[0], hostnames, hostNameSuffixMatch))
			continue
		}

		route := envoy_config_route_v3.Route{
			Match: getRouteMatch(hostnames,
				hostNameSuffixMatch,
				hRoutes[0].PathMatch,
				hRoutes[0].HeadersMatch,
				hRoutes[0].QueryParamsMatch,
				hRoutes[0].Method),
			RequestHeadersToAdd:     getHeadersToAdd(hRoutes[0].RequestHeaderFilter),
			RequestHeadersToRemove:  getHeadersToRemove(hRoutes[0].RequestHeaderFilter),
			ResponseHeadersToAdd:    getHeadersToAdd(hRoutes[0].ResponseHeaderModifier),
			ResponseHeadersToRemove: getHeadersToRemove(hRoutes[0].ResponseHeaderModifier),
		}

		if hRoutes[0].RequestRedirect != nil {
			route.Action = getRouteRedirect(hRoutes[0].RequestRedirect, listenerPort)
		} else {
			route.Action = getRouteAction(&r, backends, r.BackendHTTPFilters, r.Rewrite, r.RequestMirrors)
		}
		// If there is only one backend, we can add the header filter to the route
		if len(backends) == 1 {
			for _, fn := range hRoutes[0].BackendHTTPFilters {
				route.RequestHeadersToAdd = append(route.RequestHeadersToAdd, getHeadersToAdd(fn.RequestHeaderFilter)...)
				route.RequestHeadersToRemove = append(route.RequestHeadersToRemove, getHeadersToRemove(fn.RequestHeaderFilter)...)
				route.ResponseHeadersToAdd = append(route.ResponseHeadersToAdd, getHeadersToAdd(fn.ResponseHeaderModifier)...)
				route.ResponseHeadersToRemove = append(route.ResponseHeadersToRemove, getHeadersToRemove(fn.ResponseHeaderModifier)...)
			}
		}
		routes = append(routes, &route)
		delete(matchBackendMap, r.GetMatchKey())
	}
	return routes
}

func getRouteMatch(hostnames []string, hostNameSuffixMatch bool, pathMatch model.StringMatch, headers []model.KeyValueMatch, query []model.KeyValueMatch, method *string) *envoy_config_route_v3.RouteMatch {
	headerMatchers := getHeaderMatchers(hostnames, hostNameSuffixMatch, headers, method)
	queryMatchers := getQueryMatchers(query)

	switch {
	case pathMatch.Exact != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Path{
				Path: pathMatch.Exact,
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Prefix == "/":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{
				Prefix: pathMatch.Prefix,
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Prefix != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_PathSeparatedPrefix{
				PathSeparatedPrefix: strings.TrimSuffix(pathMatch.Prefix, "/"),
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Regex != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_SafeRegex{
				SafeRegex: &envoy_type_matcher_v3.RegexMatcher{
					Regex: pathMatch.Regex,
				},
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	default:
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{
				Prefix: "/",
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	}
}

func getQueryMatchers(query []model.KeyValueMatch) []*envoy_config_route_v3.QueryParameterMatcher {
	res := make([]*envoy_config_route_v3.QueryParameterMatcher, 0, len(query))
	for _, q := range query {
		res = append(res, &envoy_config_route_v3.QueryParameterMatcher{
			Name: q.Key,
			QueryParameterMatchSpecifier: &envoy_config_route_v3.QueryParameterMatcher_StringMatch{
				StringMatch: getEnvoyStringMatcher(q.Match),
			},
		})
	}
	return res
}

func getMatchingHeaderRegex(host string) string {
	if strings.HasPrefix(host, starDot) {
		return fmt.Sprintf("^%s+%s%s$", notDotRegex, dotRegex, strings.ReplaceAll(host[2:], dot, dotRegex))
	}
	return fmt.Sprintf("^%s$", strings.ReplaceAll(host, dot, dotRegex))
}

func getHeaderMatchers(hostnames []string, hostNameSuffixMatch bool, headers []model.KeyValueMatch, method *string) []*envoy_config_route_v3.HeaderMatcher {
	var result []*envoy_config_route_v3.HeaderMatcher

	if !hostNameSuffixMatch {
		for _, host := range hostnames {
			if len(host) != 0 && host != wildCard && strings.Contains(host, wildCard) {
				result = append(result, &envoy_config_route_v3.HeaderMatcher{
					Name: envoyAuthority,
					HeaderMatchSpecifier: &envoy_config_route_v3.HeaderMatcher_StringMatch{
						StringMatch: &envoy_type_matcher_v3.StringMatcher{
							MatchPattern: &envoy_type_matcher_v3.StringMatcher_SafeRegex{
								SafeRegex: &envoy_type_matcher_v3.RegexMatcher{
									Regex: getMatchingHeaderRegex(host),
								},
							},
						},
					},
				})
			}
		}
	}

	for _, h := range headers {
		result = append(result, &envoy_config_route_v3.HeaderMatcher{
			Name: h.Key,
			HeaderMatchSpecifier: &envoy_config_route_v3.HeaderMatcher_StringMatch{
				StringMatch: getEnvoyStringMatcher(h.Match),
			},
		})
	}

	if method != nil {
		result = append(result, &envoy_config_route_v3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &envoy_config_route_v3.HeaderMatcher_StringMatch{
				StringMatch: &envoy_type_matcher_v3.StringMatcher{
					MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
						Exact: strings.ToUpper(*method),
					},
				},
			},
		})
	}

	return result
}

func getEnvoyStringMatcher(s model.StringMatch) *envoy_type_matcher_v3.StringMatcher {
	if s.Exact != "" {
		return &envoy_type_matcher_v3.StringMatcher{
			MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
				Exact: s.Exact,
			},
		}
	}
	if s.Prefix != "" {
		return &envoy_type_matcher_v3.StringMatcher{
			MatchPattern: &envoy_type_matcher_v3.StringMatcher_Prefix{
				Prefix: s.Prefix,
			},
		}
	}
	if s.Regex != "" {
		return &envoy_type_matcher_v3.StringMatcher{
			MatchPattern: &envoy_type_matcher_v3.StringMatcher_SafeRegex{
				SafeRegex: &envoy_type_matcher_v3.RegexMatcher{
					Regex: s.Regex,
				},
			},
		}
	}
	return nil
}

func envoyHTTPRouteNoBackend(route model.HTTPRoute, hostnames []string, hostNameSuffixMatch bool) *envoy_config_route_v3.Route {
	if route.DirectResponse == nil {
		return nil
	}

	return &envoy_config_route_v3.Route{
		Match: getRouteMatch(hostnames,
			hostNameSuffixMatch,
			route.PathMatch,
			route.HeadersMatch,
			route.QueryParamsMatch,
			route.Method),
		Action: &envoy_config_route_v3.Route_DirectResponse{
			DirectResponse: &envoy_config_route_v3.DirectResponseAction{
				Status: uint32(route.DirectResponse.StatusCode),
				Body: &envoy_config_core_v3.DataSource{
					Specifier: &envoy_config_core_v3.DataSource_InlineString{
						InlineString: route.DirectResponse.Body,
					},
				},
			},
		},
	}
}

func getRouteMatch(hostnames []string, hostNameSuffixMatch bool, pathMatch model.StringMatch, headers []model.KeyValueMatch, query []model.KeyValueMatch, method *string) *envoy_config_route_v3.RouteMatch {
	headerMatchers := getHeaderMatchers(hostnames, hostNameSuffixMatch, headers, method)
	queryMatchers := getQueryMatchers(query)

	switch {
	case pathMatch.Exact != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Path{
				Path: pathMatch.Exact,
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Prefix == "/":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{
				Prefix: pathMatch.Prefix,
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Prefix != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_PathSeparatedPrefix{
				PathSeparatedPrefix: strings.TrimSuffix(pathMatch.Prefix, "/"),
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	case pathMatch.Regex != "":
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_SafeRegex{
				SafeRegex: &envoy_type_matcher_v3.RegexMatcher{
					Regex: pathMatch.Regex,
				},
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	default:
		return &envoy_config_route_v3.RouteMatch{
			PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{
				Prefix: "/",
			},
			Headers:         headerMatchers,
			QueryParameters: queryMatchers,
		}
	}
}

func getHeadersToAdd(filter *model.HTTPHeaderFilter) []*envoy_config_core_v3.HeaderValueOption {
	if filter == nil {
		return nil
	}
	result := make(
		[]*envoy_config_core_v3.HeaderValueOption,
		0,
		len(filter.HeadersToAdd)+len(filter.HeadersToSet),
	)
	for _, h := range filter.HeadersToAdd {
		result = append(result, &envoy_config_core_v3.HeaderValueOption{
			Header: &envoy_config_core_v3.HeaderValue{
				Key:   h.Name,
				Value: h.Value,
			},
			AppendAction: envoy_config_core_v3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
		})
	}

	for _, h := range filter.HeadersToSet {
		result = append(result, &envoy_config_core_v3.HeaderValueOption{
			Header: &envoy_config_core_v3.HeaderValue{
				Key:   h.Name,
				Value: h.Value,
			},
			AppendAction: envoy_config_core_v3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	return result
}

func getHeadersToRemove(filter *model.HTTPHeaderFilter) []string {
	if filter == nil {
		return nil
	}
	return filter.HeadersToRemove
}
