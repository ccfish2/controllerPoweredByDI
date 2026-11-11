package ingress

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	envoy_config_cluster_v3 "github.com/cilium/proxy/go/envoy/config/cluster/v3"
	envoy_config_listener "github.com/cilium/proxy/go/envoy/config/listener/v3"
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
	networkingv1 "k8s.io/api/networking/v1"
	k8sApisErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/ccfish2/controllerPoweredByDI/pkg/envoy"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

func testCluster(t *testing.T, name, edsName string) dolphinv1.XDSResource {
	t.Helper()
	a, err := anypb.New(&envoy_config_cluster_v3.Cluster{
		Name:             name,
		EdsClusterConfig: &envoy_config_cluster_v3.Cluster_EdsClusterConfig{ServiceName: edsName},
	})
	require.NoError(t, err)
	return dolphinv1.XDSResource{Any: a}
}

func testRouteConfig(t *testing.T, clusters ...string) dolphinv1.XDSResource {
	t.Helper()
	var routes []*envoy_config_route_v3.Route
	for i, c := range clusters {
		prefix := "/"
		if i == 0 && len(clusters) > 1 {
			prefix = "/details"
		}
		routes = append(routes, &envoy_config_route_v3.Route{
			Match: &envoy_config_route_v3.RouteMatch{
				PathSpecifier: &envoy_config_route_v3.RouteMatch_Prefix{Prefix: prefix},
			},
			Action: &envoy_config_route_v3.Route_Route{
				Route: &envoy_config_route_v3.RouteAction{
					ClusterSpecifier: &envoy_config_route_v3.RouteAction_Cluster{Cluster: c},
				},
			},
		})
	}
	a, err := anypb.New(&envoy_config_route_v3.RouteConfiguration{
		Name:         "listener-insecure",
		VirtualHosts: []*envoy_config_route_v3.VirtualHost{{Name: "*", Domains: []string{"*"}, Routes: routes}},
	})
	require.NoError(t, err)
	return dolphinv1.XDSResource{Any: a}
}

func routeClusters(t *testing.T, res dolphinv1.XDSResource) []string {
	t.Helper()
	require.Equal(t, envoy.RouteTypeURL, res.Any.GetTypeUrl())
	rc := &envoy_config_route_v3.RouteConfiguration{}
	require.NoError(t, res.Any.UnmarshalTo(rc))
	var out []string
	for _, vh := range rc.VirtualHosts {
		for _, r := range vh.Routes {
			out = append(out, r.GetRoute().GetCluster())
		}
	}
	return out
}

// This is the situation of dolphinenvoyconfig-basic-ingress.yaml: the routes point to
// dolphin:<svc>:9080 but the clusters are <svc>:<svc>:9080.
func TestFixDolphinEnvoyConfig(t *testing.T) {
	t.Run("route pointing at a non-existing cluster is re-pointed", func(t *testing.T) {
		dec := &dolphinv1.DolphinEnvoyConfig{
			Spec: dolphinv1.DolphinEnvoyConfigSpec{
				Resources: []dolphinv1.XDSResource{
					testRouteConfig(t, "dolphin:details:9080", "dolphin:productpage:9080"),
					testCluster(t, "details:details:9080", "details/details:9080"),
					testCluster(t, "productpage:productpage:9080", "productpage/productpage:9080"),
				},
			},
		}

		require.NoError(t, fixDolphinEnvoyConfig(dec))

		assert.Equal(t, []string{"details:details:9080", "productpage:productpage:9080"}, routeClusters(t, dec.Spec.Resources[0]))
	})

	t.Run("consistent config is left untouched", func(t *testing.T) {
		route := testRouteConfig(t, "default:details:9080")
		dec := &dolphinv1.DolphinEnvoyConfig{
			Spec: dolphinv1.DolphinEnvoyConfigSpec{
				Resources: []dolphinv1.XDSResource{route, testCluster(t, "default:details:9080", "default/details:9080")},
			},
		}

		require.NoError(t, fixDolphinEnvoyConfig(dec))

		assert.Same(t, route.Any, dec.Spec.Resources[0].Any)
	})

	t.Run("unresolvable and ambiguous references are errors", func(t *testing.T) {
		unknown := &dolphinv1.DolphinEnvoyConfig{
			Spec: dolphinv1.DolphinEnvoyConfigSpec{
				Resources: []dolphinv1.XDSResource{
					testRouteConfig(t, "dolphin:nothing:9080"),
					testCluster(t, "default:details:9080", "default/details:9080"),
				},
			},
		}
		require.Error(t, fixDolphinEnvoyConfig(unknown))

		ambiguous := &dolphinv1.DolphinEnvoyConfig{
			Spec: dolphinv1.DolphinEnvoyConfigSpec{
				Resources: []dolphinv1.XDSResource{
					testRouteConfig(t, "dolphin:details:9080"),
					testCluster(t, "a:details:9080", "a/details:9080"),
					testCluster(t, "b:details:9080", "b/details:9080"),
				},
			},
		}
		require.Error(t, fixDolphinEnvoyConfig(ambiguous))
	})

	t.Run("EDS clusters get their service added to backendServices", func(t *testing.T) {
		dec := &dolphinv1.DolphinEnvoyConfig{
			Spec: dolphinv1.DolphinEnvoyConfigSpec{
				BackendServices: []*dolphinv1.Service{{Name: "details", Namespace: "details", Ports: []string{"9080"}}},
				Resources: []dolphinv1.XDSResource{
					testCluster(t, "details:details:9080", "details/details:9080"),
					testCluster(t, "productpage:productpage:9080", "productpage/productpage:9080"),
				},
			},
		}

		require.NoError(t, fixDolphinEnvoyConfig(dec))

		assert.Equal(t, []*dolphinv1.Service{
			{Name: "details", Namespace: "details", Ports: []string{"9080"}},
			{Name: "productpage", Namespace: "productpage", Ports: []string{"9080"}},
		}, dec.Spec.BackendServices)
	})
}

func TestToCiliumEnvoyConfig(t *testing.T) {
	dec := &dolphinv1.DolphinEnvoyConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dolphin-ingress-dolphin-basic-ingress",
			Namespace: "dolphin",
			Labels:    map[string]string{"dolphin.io/use-original-source-address": "false", "other": "label"},
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "networking.k8s.io/v1", Kind: "Ingress", Name: "basic-ingress", UID: "uid", Controller: model.AddressOf(true)},
			},
		},
		Spec: dolphinv1.DolphinEnvoyConfigSpec{
			Services:        []*dolphinv1.ServiceListener{{Name: "dolphin-ingress-basic-ingress", Namespace: "dolphin", Ports: []uint16{80}}},
			BackendServices: []*dolphinv1.Service{{Name: "details", Namespace: "details", Ports: []string{"9080"}}},
			Resources: []dolphinv1.XDSResource{
				testRouteConfig(t, "details:details:9080"),
				testCluster(t, "details:details:9080", "details/details:9080"),
			},
		},
	}

	cec, err := toCiliumEnvoyConfig(dec)
	require.NoError(t, err)

	assert.Equal(t, "cilium.io/v2", cec.GetAPIVersion())
	assert.Equal(t, "CiliumEnvoyConfig", cec.GetKind())
	assert.Equal(t, dec.Name, cec.GetName())
	assert.Equal(t, dec.Namespace, cec.GetNamespace())
	assert.Equal(t, dec.OwnerReferences, cec.GetOwnerReferences())
	assert.Equal(t, map[string]string{"cilium.io/use-original-source-address": "false", "other": "label"}, cec.GetLabels())

	services, _, err := unstructured.NestedSlice(cec.Object, "spec", "services")
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"name": "dolphin-ingress-basic-ingress", "namespace": "dolphin", "ports": []any{int64(80)}}}, services)

	backends, _, err := unstructured.NestedSlice(cec.Object, "spec", "backendServices")
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"name": "details", "namespace": "details", "number": []any{"9080"}}}, backends)

	resources, _, err := unstructured.NestedSlice(cec.Object, "spec", "resources")
	require.NoError(t, err)
	require.Len(t, resources, 2)
	assert.Equal(t, "type.googleapis.com/envoy.config.route.v3.RouteConfiguration", resources[0].(map[string]any)["@type"])
	assert.Equal(t, "type.googleapis.com/envoy.config.cluster.v3.Cluster", resources[1].(map[string]any)["@type"])
}

func TestReconcileCreatesCiliumEnvoyConfig(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	newIngress := func() *networkingv1.Ingress {
		return &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "test"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: model.AddressOf("dolphin"),
				DefaultBackend:   defaultBackend(),
			},
		}
	}

	t.Run("dedicated mode creates a CiliumEnvoyConfig next to the DolphinEnvoyConfig", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(newIngress()).Build()
		r := newIngressReconciler(logger, c, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)

		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test"}})
		require.NoError(t, err)

		cec := newCiliumEnvoyConfig("", "")
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test-test"}, cec))
		assert.Equal(t, "false", cec.GetLabels()[ciliumUseOriginalSourceAddressLabel])
		require.Len(t, cec.GetOwnerReferences(), 1)
		assert.Equal(t, "Ingress", cec.GetOwnerReferences()[0].Kind)

		resources, _, err := unstructured.NestedSlice(cec.Object, "spec", "resources")
		require.NoError(t, err)
		assert.NotEmpty(t, resources)

		// The cilium-envoy data plane must get neither the PROXY protocol listener
		// filter nor an explicit cluster connectTimeout (see generated-cec.yaml).
		raw, err := json.Marshal(cec.Object)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "proxy_protocol")
		assert.NotContains(t, string(raw), "connectTimeout")
	})

	t.Run("PROXY protocol is never generated, even when it is requested", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(newIngress()).Build()
		// useProxyProtocol=true is what the old default / a leftover flag would pass in
		r := newIngressReconciler(logger, c, testDolphinNamespace, testEnforceHTTPS, true, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)

		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test"}})
		require.NoError(t, err)

		key := types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test-test"}

		dec := &dolphinv1.DolphinEnvoyConfig{}
		require.NoError(t, c.Get(context.Background(), key, dec))
		listeners := 0
		for _, res := range dec.Spec.Resources {
			if res.Any.GetTypeUrl() != envoy.ListenerTypeURL {
				continue
			}
			listeners++
			l := &envoy_config_listener.Listener{}
			require.NoError(t, res.Any.UnmarshalTo(l))
			for _, f := range l.GetListenerFilters() {
				assert.NotEqual(t, "envoy.filters.listener.proxy_protocol", f.GetName())
			}
		}
		require.NotZero(t, listeners, "expected a listener in the DolphinEnvoyConfig")

		cec := newCiliumEnvoyConfig("", "")
		require.NoError(t, c.Get(context.Background(), key, cec))
		raw, err := json.Marshal(cec.Object)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "proxy_protocol")
	})

	t.Run("deleting the Ingress removes the dedicated CiliumEnvoyConfig", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(newIngress()).Build()
		r := newIngressReconciler(logger, c, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)
		req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "test", Name: "test"}}

		_, err := r.Reconcile(context.Background(), req)
		require.NoError(t, err)

		require.NoError(t, r.tryCleanupDedicatedResources(context.Background(), req.NamespacedName))

		err = c.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test-test"}, newCiliumEnvoyConfig("", ""))
		require.True(t, k8sApisErrors.IsNotFound(err))
	})
}
