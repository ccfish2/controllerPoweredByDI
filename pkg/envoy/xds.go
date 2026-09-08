package envoy

import (
	envoy_config_core "github.com/cilium/proxy/go/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// ListenerTypeURL is the type URL of Listener resources.
	ListenerTypeURL = "type.googleapis.com/envoy.config.listener.v3.Listener"

	// RouteTypeURL is the type URL of HTTP Route resources.
	RouteTypeURL = "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"

	// ClusterTypeURL is the type URL of Cluster resources.
	ClusterTypeURL = "type.googleapis.com/envoy.config.cluster.v3.Cluster"

	// HttpConnectionManagerTypeURL is the type URL of HttpConnectionManager filter.
	HttpConnectionManagerTypeURL = "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"

	// TCPProxyTypeURL is the type URL of TCPProxy filter.
	TCPProxyTypeURL = "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy"

	// EndpointTypeURL is the type URL of Endpoint resources.
	EndpointTypeURL = "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"

	// SecretTypeURL is the type URL of Endpoint resources.
	SecretTypeURL = "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret"

	// DownstreamTlsContextURL is the type URL of DownstreamTlsContext.
	DownstreamTlsContextURL = "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext"
)

func GetInternalListenerCIDRs(ipv4, ipv6 bool) []*envoy_config_core.CidrRange {
	var cidrRanges []*envoy_config_core.CidrRange
	if ipv4 {
		cidrRanges = append(cidrRanges,
			[]*envoy_config_core.CidrRange{
				{AddressPrefix: "10.0.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: 8}},
				{AddressPrefix: "172.16.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: 12}},
				{AddressPrefix: "192.168.0.0", PrefixLen: &wrapperspb.UInt32Value{Value: 16}},
				{AddressPrefix: "127.0.0.1", PrefixLen: &wrapperspb.UInt32Value{Value: 32}},
			}...,
		)
	}
	if ipv6 {
		cidrRanges = append(cidrRanges, &envoy_config_core.CidrRange{
			AddressPrefix: "::1",
			PrefixLen:     &wrapperspb.UInt32Value{Value: 128},
		})
	}
	return cidrRanges
}
