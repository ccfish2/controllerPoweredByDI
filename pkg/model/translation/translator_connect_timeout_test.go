package translation

import (
	"testing"

	envoy_config_cluster_v3 "github.com/cilium/proxy/go/envoy/config/cluster/v3"
	"github.com/stretchr/testify/require"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
)

func httpClusterFor(t *testing.T, tr *defaultTranslator) *envoy_config_cluster_v3.Cluster {
	t.Helper()

	m := &model.Model{
		HTTP: []model.HTTPListener{{
			Hostname: "*",
			Port:     80,
			Routes: []model.HTTPRoute{{
				Backends: []model.Backend{{
					Name:      "details",
					Namespace: "dolphin",
					Port:      &model.BackendPort{Port: 9080},
				}},
			}},
		}},
	}

	clusters := tr.getClusters(m)
	require.Len(t, clusters, 1)

	c := &envoy_config_cluster_v3.Cluster{}
	require.NoError(t, clusters[0].Any.UnmarshalTo(c))
	return c
}

func TestGetClustersConnectTimeout(t *testing.T) {
	t.Run("default keeps the explicit 5s connect timeout (Gateway API)", func(t *testing.T) {
		tr := NewTranslator("n", "ns", "secrets", false, false, false, 60, true, false).(*defaultTranslator)

		c := httpClusterFor(t, tr)

		require.NotNil(t, c.ConnectTimeout)
		require.Equal(t, int64(5), c.ConnectTimeout.Seconds)
	})

	t.Run("WithoutClusterConnectTimeout leaves it unset (Ingress)", func(t *testing.T) {
		tr := NewTranslator("n", "ns", "secrets", false, false, false, 60, true, false, WithoutClusterConnectTimeout()).(*defaultTranslator)

		c := httpClusterFor(t, tr)

		require.Nil(t, c.ConnectTimeout)
		// the rest of the cluster is unchanged
		require.Equal(t, "dolphin:details:9080", c.Name)
		require.Equal(t, "dolphin/details:9080", c.GetEdsClusterConfig().GetServiceName())
	})
}
