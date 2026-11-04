package translation

import (
	envoy_config_cluster_v3 "github.com/cilium/proxy/go/envoy/config/cluster/v3"
	envoy_config_core_v3 "github.com/cilium/proxy/go/envoy/config/core/v3"
	envoy_extensions_transport_sockets_tls_v3 "github.com/cilium/proxy/go/envoy/extensions/transport_sockets/tls/v3"
	envoy_upstreams_http_v3 "github.com/cilium/proxy/go/envoy/extensions/upstreams/http/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/ccfish2/controllerPoweredByDI/pkg/envoy"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

const (
	httpProtocolOptionsType = "envoy.extensions.upstreams.http.v3.HttpProtocolOptions"
	upstreamTLSType         = "envoy.transport_sockets.tls"
)

type HTTPVersionType int

const (
	HTTPVersionDownstream HTTPVersionType = -1
	HTTPVersionAuto       HTTPVersionType = 0
	HTTPVersion1          HTTPVersionType = 1
	HTTPVersion2          HTTPVersionType = 2
	HTTPVersion3          HTTPVersionType = 3
)

func WithClusterLbPolicy(lbPolicy int32) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		if cluster == nil {
			return cluster
		}
		cluster.LbPolicy = envoy_config_cluster_v3.Cluster_LbPolicy(lbPolicy)
		return cluster
	}
}

func WithOutlierDetection(splitExternalLocalOriginErrors bool) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		if cluster == nil {
			return cluster
		}
		cluster.OutlierDetection = &envoy_config_cluster_v3.OutlierDetection{
			SplitExternalLocalOriginErrors: splitExternalLocalOriginErrors,
		}
		return cluster
	}
}

func WithProtocol(protocolVersion HTTPVersionType) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		options := &envoy_upstreams_http_v3.HttpProtocolOptions{}
		switch protocolVersion {
		case HTTPVersion1, HTTPVersionAuto:
			options.UpstreamProtocolOptions = &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_HttpProtocolOptions{},
				},
			}
		case HTTPVersion2:
			options.UpstreamProtocolOptions = &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{},
				},
			}
		case HTTPVersion3:
			options.UpstreamProtocolOptions = &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_ExplicitHttpConfig_Http3ProtocolOptions{},
				},
			}
		}

		cluster.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			httpProtocolOptionsType: toAny(options),
		}
		return cluster
	}
}

type ClusterMutator func(*envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster

// WithConnectionTimeout sets the cluster's connection timeout.
func WithConnectionTimeout(seconds int) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		if cluster == nil {
			return cluster
		}
		cluster.ConnectTimeout = &durationpb.Duration{Seconds: int64(seconds)}
		return cluster
	}
}

// WithIdleTimeout sets the cluster's connection idle timeout.
func WithIdleTimeout(seconds int) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		if cluster == nil {
			return cluster
		}
		a := cluster.TypedExtensionProtocolOptions[httpProtocolOptionsType]
		opts := &envoy_upstreams_http_v3.HttpProtocolOptions{}
		if err := a.UnmarshalTo(opts); err != nil {
			return cluster
		}
		opts.CommonHttpProtocolOptions = &envoy_config_core_v3.HttpProtocolOptions{
			IdleTimeout: &durationpb.Duration{Seconds: int64(seconds)},
		}
		cluster.TypedExtensionProtocolOptions[httpProtocolOptionsType] = toAny(opts)
		return cluster
	}
}

func WithUpstreamTLS(secretsNamespace, validationSecret, sni string) ClusterMutator {
	return func(cluster *envoy_config_cluster_v3.Cluster) *envoy_config_cluster_v3.Cluster {
		if cluster == nil {
			return cluster
		}

		validationContext := &envoy_extensions_transport_sockets_tls_v3.UpstreamTlsContext{
			Sni: sni,
			CommonTlsContext: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext{
				ValidationContextType: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &envoy_extensions_transport_sockets_tls_v3.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext: &envoy_extensions_transport_sockets_tls_v3.CertificateValidationContext{},
						ValidationContextSdsSecretConfig: &envoy_extensions_transport_sockets_tls_v3.SdsSecretConfig{
							Name: secretsNamespace + "/" + validationSecret,
						},
					},
				},
				TlsParams: &envoy_extensions_transport_sockets_tls_v3.TlsParameters{
					TlsMaximumProtocolVersion: envoy_extensions_transport_sockets_tls_v3.TlsParameters_TLSv1_3,
				},
			},
		}
		tlsBytes, err := proto.Marshal(validationContext)
		if err != nil {
			return cluster
		}
		cluster.TransportSocket = &envoy_config_core_v3.TransportSocket{
			Name: upstreamTLSType,
			ConfigType: &envoy_config_core_v3.TransportSocket_TypedConfig{
				TypedConfig: &anypb.Any{
					TypeUrl: envoy.UpstreamTlsContextURL,
					Value:   tlsBytes,
				},
			},
		}
		return cluster
	}
}

func NewHTTPCluster(clusterName string, clusterServiceName string, mutationFunc ...ClusterMutator) (dolphinv1.XDSResource, error) {
	cluster := &envoy_config_cluster_v3.Cluster{
		Name: clusterName,
		TypedExtensionProtocolOptions: map[string]*anypb.Any{
			httpProtocolOptionsType: toAny(&envoy_upstreams_http_v3.HttpProtocolOptions{
				UpstreamProtocolOptions: &envoy_upstreams_http_v3.HttpProtocolOptions_UseDownstreamProtocolConfig{
					UseDownstreamProtocolConfig: &envoy_upstreams_http_v3.HttpProtocolOptions_UseDownstreamHttpConfig{
						Http2ProtocolOptions: &envoy_config_core_v3.Http2ProtocolOptions{},
					},
				},
			}),
		},
		ClusterDiscoveryType: &envoy_config_cluster_v3.Cluster_Type{Type: envoy_config_cluster_v3.Cluster_EDS},
		EdsClusterConfig:     &envoy_config_cluster_v3.Cluster_EdsClusterConfig{ServiceName: clusterServiceName},
	}

	for _, fn := range mutationFunc {
		cluster = fn(cluster)
	}
	clusterBytes, err := proto.Marshal(cluster)
	if err != nil {
		return dolphinv1.XDSResource{}, err
	}
	return dolphinv1.XDSResource{Any: &anypb.Any{TypeUrl: envoy.ClusterTypeURL, Value: clusterBytes}}, nil
}

func NewTCPClusterWithDefaults(clusterName string, clusterServiceName string, mutationFunc ...ClusterMutator) (dolphinv1.XDSResource, error) {
	fns := append(mutationFunc,
		WithConnectionTimeout(5),
		WithClusterLbPolicy(int32(envoy_config_cluster_v3.Cluster_ROUND_ROBIN)),
		WithOutlierDetection(true),
	)
	return NewTCPCluster(clusterName, clusterServiceName, fns...)
}

func NewTCPCluster(clusterName string, clusterServiceName string, mutationFunc ...ClusterMutator) (dolphinv1.XDSResource, error) {
	cluster := &envoy_config_cluster_v3.Cluster{
		Name:                 clusterName,
		ClusterDiscoveryType: &envoy_config_cluster_v3.Cluster_Type{Type: envoy_config_cluster_v3.Cluster_EDS},
		EdsClusterConfig:     &envoy_config_cluster_v3.Cluster_EdsClusterConfig{ServiceName: clusterServiceName},
	}
	for _, fn := range mutationFunc {
		cluster = fn(cluster)
	}
	clusterBytes, err := proto.Marshal(cluster)
	if err != nil {
		return dolphinv1.XDSResource{}, err
	}
	return dolphinv1.XDSResource{Any: &anypb.Any{TypeUrl: envoy.ClusterTypeURL, Value: clusterBytes}}, nil
}
