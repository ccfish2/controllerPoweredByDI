package translation

import (
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ccfish2/infra/pkg/envoy"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

type RouteConfigurationMutator func(*envoy_config_route_v3.RouteConfiguration) *envoy_config_route_v3.RouteConfiguration

// NewRouteConfiguration returns a new route configuration for a given list of http routes.
func NewRouteConfiguration(name string, virtualhosts []*envoy_config_route_v3.VirtualHost, mutators ...RouteConfigurationMutator) (dolphinv1.XDSResource, error) {
	routeConfig := &envoy_config_route_v3.RouteConfiguration{
		Name:         name,
		VirtualHosts: virtualhosts,
	}

	for _, fn := range mutators {
		routeConfig = fn(routeConfig)
	}

	routeBytes, err := proto.Marshal(routeConfig)
	if err != nil {
		return dolphinv1.XDSResource{}, err
	}

	return dolphinv1.XDSResource{
		Any: &anypb.Any{
			TypeUrl: envoy.RouteTypeURL,
			Value:   routeBytes,
		},
	}, nil
}
