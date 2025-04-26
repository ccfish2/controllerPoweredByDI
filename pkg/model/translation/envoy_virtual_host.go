package translation

import (
	"net"
	"sort"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"
)

type VirtualHostParameter struct {
	HostNames           []string
	HTTPSRedirect       bool
	HostNameSuffixMatch bool
	ListenerPort        uint32
}

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
