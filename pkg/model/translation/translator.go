package translation

import (
	"cmp"
	"fmt"
	goslices "slices"
	"sort"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envoy_config_cluster_v3 "github.com/cilium/proxy/go/envoy/config/cluster/v3"
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"

	v1 "k8s.io/api/core/v1"

	// dolphin
	"github.com/ccfish2/infra/pkg/k8s"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/slices"
)

const (
	secureHost   = "secure"
	insecureHost = "insecure"
)

type defaultTranslator struct {
	name                string
	namespace           string
	secretsNamespace    string
	enforceHTTPs        bool
	useProxyProtocol    bool
	hostNameSuffixMatch bool
	idleTimeoutSeconds  int

	ipv4Enabled bool
	ipv6Enabled bool
}

var _ Translator = (*defaultTranslator)(nil)

func NewTranslator(name, namespace, secretsNamespace string, enforceHTTPs bool, useProxyProtocol bool,
	hostNameSuffixMatch bool, idleTimeoutSeconds int,
	ipv4Enabled bool, ipv6Enabled bool) Translator {
	return &defaultTranslator{
		name:                name,
		namespace:           namespace,
		secretsNamespace:    secretsNamespace,
		enforceHTTPs:        enforceHTTPs,
		useProxyProtocol:    useProxyProtocol,
		hostNameSuffixMatch: hostNameSuffixMatch,
		idleTimeoutSeconds:  idleTimeoutSeconds,
		ipv4Enabled:         ipv4Enabled,
		ipv6Enabled:         ipv6Enabled,
	}
}

// translate translates the model into a DolphinEnvoyConfig
func (d *defaultTranslator) Translate(m *model.Model, _ ...*dolphinv2alpha1.DolphinGatewayClassConfig) (*dolphinv1.DolphinEnvoyConfig, *v1.Service, *v1.Endpoints, error) {
	dec := &dolphinv1.DolphinEnvoyConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.name,
			Namespace: d.namespace,
			Labels: map[string]string{
				k8s.UseOriginalSourceAddressLabel: "false",
			},
		},
	}
	dec.Spec.BackendServices = d.getBackendServices(m)
	dec.Spec.Services = d.getServices(m)
	dec.Spec.Resources = d.getResources(m)
	return dec, nil, nil, nil
}

func (d *defaultTranslator) getBackendServices(m *model.Model) []*dolphinv1.Service {
	var res []*dolphinv1.Service

	for ns, v := range getNamespaceNamePortsMap(m) {
		for name, ports := range v {
			res = append(res, &dolphinv1.Service{
				Name:      name,
				Namespace: ns,
				Ports:     ports,
			})
		}
	}

	// Make sure the result is sorted by namespace and name to avoid any
	// nondeterministic behavior.
	sort.Slice(res, func(i, j int) bool {
		if res[i].Namespace != res[j].Namespace {
			return res[i].Namespace < res[j].Namespace
		}
		if res[i].Name != res[j].Name {
			return res[i].Name < res[j].Name
		}
		return res[i].Ports[0] < res[j].Ports[0]
	})
	return res
}

func (i *defaultTranslator) getServices(m *model.Model) []*dolphinv1.ServiceListener {
	ports := map[uint16]struct{}{}
	if m != nil {
		for _, listener := range m.HTTP {
			if listener.Port != 0 {
				ports[uint16(listener.Port)] = struct{}{}
			}
		}
		for _, listener := range m.TLS {
			if listener.Port != 0 {
				ports[uint16(listener.Port)] = struct{}{}
			}
		}
		for _, listener := range m.TCP {
			if listener.Port != 0 {
				ports[uint16(listener.Port)] = struct{}{}
			}
		}
	}
	listenerPorts := make([]uint16, 0, len(ports))
	for port := range ports {
		listenerPorts = append(listenerPorts, port)
	}
	goslices.Sort(listenerPorts)
	if len(listenerPorts) == 0 {
		listenerPorts = nil
	}

	return []*dolphinv1.ServiceListener{
		{
			Name:      i.name,
			Namespace: i.namespace,
			Ports:     listenerPorts,
		},
	}
}

func (i *defaultTranslator) getListener(m *model.Model) []dolphinv1.XDSResource {
	if len(m.HTTP) == 0 && len(m.TLS) == 0 {
		return nil
	}

	mutatorFuncs := []ListenerMutator{}
	if i.useProxyProtocol {
		mutatorFuncs = append(mutatorFuncs, WithProxyProtocol())
	}

	l, _ := newListenerWithDefaults("listener", i.secretsNamespace, len(m.HTTP) > 0, tlsSecretsToHostnames(m.HTTP),
		tlsPassthroughBackendsToHostnames(m.TLS), i.ipv4Enabled, i.ipv6Enabled, mutatorFuncs...)
	return []dolphinv1.XDSResource{l}
}

func tlsPassthroughBackendsToHostnames(tlsListeners []model.TLSListener) map[string][]string {
	tlsPassthroughBackendsToHostnames := make(map[string][]string)
	for _, h := range tlsListeners {
		for _, route := range h.Routes {
			for _, backend := range route.Backends {
				key := fmt.Sprintf("%s:%s:%s", backend.Namespace, backend.Name, backend.Port.GetPort())
				tlsPassthroughBackendsToHostnames[key] = append(tlsPassthroughBackendsToHostnames[key], route.Hostnames...)
			}
		}
	}

	return tlsPassthroughBackendsToHostnames
}

func tlsSecretsToHostnames(httpListeners []model.HTTPListener) map[model.TLSSecret][]string {
	tlsSecretsToHostnames := make(map[model.TLSSecret][]string)
	for _, h := range httpListeners {
		for _, s := range h.TLS {
			tlsSecretsToHostnames[s] = append(tlsSecretsToHostnames[s], h.Hostname)
		}
	}

	return tlsSecretsToHostnames
}

func (i *defaultTranslator) getResources(m *model.Model) []dolphinv1.XDSResource {
	var res []dolphinv1.XDSResource
	res = append(res, i.getListener(m)...)
	res = append(res, i.getEnvoyHTTPRouteConfiguration(m)...)
	res = append(res, i.getClusters(m)...)
	res = append(res, i.getTCPRouteListeners(m)...)
	return res
}

func (i *defaultTranslator) getHTTPRouteListener(m *model.Model) []dolphinv1.XDSResource {
	if len(m.HTTP) == 0 {
		return nil
	}
	tlsMap := make(map[model.TLSSecret][]string)
	for _, h := range m.HTTP {
		for _, s := range h.TLS {
			tlsMap[s] = append(tlsMap[s], h.Hostname)
		}
	}

	mutatorFuncs := []ListenerMutator{}
	if i.useProxyProtocol {
		mutatorFuncs = append(mutatorFuncs, WithProxyProtocol())
	}
	l, _ := NewHTTPListenerWithDefaults("listener", i.secretsNamespace, tlsMap, mutatorFuncs...)
	return []dolphinv1.XDSResource{l}
}

func (i *defaultTranslator) getTLSRouteListener(m *model.Model) []dolphinv1.XDSResource {
	if len(m.TLS) == 0 {
		return nil
	}
	backendsMap := make(map[string][]string)
	for _, h := range m.TLS {
		for _, route := range h.Routes {
			for _, backend := range route.Backends {
				key := fmt.Sprintf("%s:%s:%s", backend.Namespace, backend.Name, backend.Port.GetPort())
				backendsMap[key] = append(backendsMap[key], route.Hostnames...)
			}
		}
	}

	if len(backendsMap) == 0 {
		return nil
	}

	mutatorFuncs := []ListenerMutator{}
	if i.useProxyProtocol {
		mutatorFuncs = append(mutatorFuncs, WithProxyProtocol())
	}
	l, _ := NewSNIListenerWithDefaults("listener", backendsMap, mutatorFuncs...)
	return []dolphinv1.XDSResource{l}
}

// a hard one
func (i *defaultTranslator) getEnvoyHTTPRouteConfiguration(m *model.Model) []dolphinv1.XDSResource {
	var res []dolphinv1.XDSResource

	portHostName := map[string][]string{}
	hostNamePortRoutes := map[string]map[string][]model.HTTPRoute{}

	for _, l := range m.HTTP {
		for _, r := range l.Routes {
			port := insecureHost
			if l.TLS != nil {
				port = secureHost
			}

			if len(r.Hostnames) == 0 {
				portHostName[port] = append(portHostName[port], l.Hostname)
				if _, ok := hostNamePortRoutes[l.Hostname]; !ok {
					hostNamePortRoutes[l.Hostname] = map[string][]model.HTTPRoute{}
				}
				hostNamePortRoutes[l.Hostname][port] = append(hostNamePortRoutes[l.Hostname][port], r)
				continue
			}
			for _, h := range r.Hostnames {
				portHostName[port] = append(portHostName[port], h)
				if _, ok := hostNamePortRoutes[h]; !ok {
					hostNamePortRoutes[h] = map[string][]model.HTTPRoute{}
				}
				hostNamePortRoutes[h][port] = append(hostNamePortRoutes[h][port], r)
			}
		}
	}

	for _, port := range []string{insecureHost, secureHost} {
		hostNames, exists := portHostName[port]
		if !exists && port == insecureHost && len(m.HTTP) > 0 {
			rc, _ := NewRouteConfiguration("listener-insecure", nil)
			res = append(res, rc)
		}
		if !exists {
			continue
		}
		var virtualhosts []*envoy_config_route_v3.VirtualHost

		redirectedHost := map[string]struct{}{}

		if port == insecureHost && i.enforceHTTPs {
			for _, h := range slices.Unique(portHostName[secureHost]) {
				vhs, _ := NewVirtualHostWithDefaults(hostNamePortRoutes[h][secureHost], VirtualHostParameter{
					HostNames:           []string{h},
					HTTPSRedirect:       true,
					HostNameSuffixMatch: i.hostNameSuffixMatch,
					ListenerPort:        m.HTTP[0].Port,
				})
				virtualhosts = append(virtualhosts, vhs)
				redirectedHost[h] = struct{}{}
			}
		}
		for _, h := range slices.Unique(hostNames) {
			if port == insecureHost {
				if _, ok := redirectedHost[h]; ok {
					continue
				}
			}
			routes, exists := hostNamePortRoutes[h][port]
			if !exists {
				continue
			}
			vhs, _ := NewVirtualHostWithDefaults(routes, VirtualHostParameter{
				HostNames:           []string{h},
				HTTPSRedirect:       false,
				HostNameSuffixMatch: i.hostNameSuffixMatch,
				ListenerPort:        m.HTTP[0].Port,
			})
			virtualhosts = append(virtualhosts, vhs)
		}

		routeName := fmt.Sprintf("listener-%s", port)
		goslices.SortStableFunc(virtualhosts, func(a, b *envoy_config_route_v3.VirtualHost) int { return cmp.Compare(a.Name, b.Name) })
		rc, _ := NewRouteConfiguration(routeName, virtualhosts)
		res = append(res, rc)
	}

	return res
}

func getClusterName(ns, name, port string) string {
	return fmt.Sprintf("%s:%s:%s", ns, name, port)
}

func getClusterServiceName(ns, name, port string) string {
	return fmt.Sprintf("%s/%s:%s", ns, name, port)
}

func (i *defaultTranslator) getClusters(m *model.Model) []dolphinv1.XDSResource {
	envoyClusters := map[string]dolphinv1.XDSResource{}
	var sortedClusterNames []string

	for ns, v := range getNamespaceNamePortsMapForHTTP(m) {
		for name, ports := range v {
			for _, port := range ports {
				clusterName := getClusterName(ns, name, port)
				clusterServiceName := getClusterServiceName(ns, name, port)
				sortedClusterNames = append(sortedClusterNames, clusterName)
				mutators := []ClusterMutator{
					WithConnectionTimeout(5),
					WithIdleTimeout(i.idleTimeoutSeconds),
					WithClusterLbPolicy(int32(envoy_config_cluster_v3.Cluster_ROUND_ROBIN)),
					WithOutlierDetection(true),
				}

				if isGRPCService(m, ns, name, port) {
					mutators = append(mutators, WithProtocol(HTTPVersion2))
				}
				envoyClusters[clusterName], _ = NewHTTPCluster(clusterName, clusterServiceName, mutators...)
			}
		}
	}
	for ns, v := range getNamespaceNamePortsMapForTCP(m) {
		for name, ports := range v {
			for _, port := range ports {
				clusterName := getClusterName(ns, name, port)
				clusterServiceName := getClusterServiceName(ns, name, port)
				sortedClusterNames = append(sortedClusterNames, clusterName)
				envoyClusters[clusterName], _ = NewTCPClusterWithDefaults(clusterName, clusterServiceName)
			}
		}
	}

	sort.Strings(sortedClusterNames)
	res := make([]dolphinv1.XDSResource, len(sortedClusterNames))
	for i, name := range sortedClusterNames {
		res[i] = envoyClusters[name]
	}

	return res
}

func isGRPCService(m *model.Model, ns string, name string, port string) bool {
	var res bool

	for _, l := range m.HTTP {
		for _, r := range l.Routes {
			if !r.IsGRPC {
				continue
			}
			for _, be := range r.Backends {
				if be.Name == name && be.Namespace == ns && be.Port != nil && be.Port.GetPort() == port {
					return true
				}
			}
		}
	}
	return res
}

func getNamespaceNamePortsMap(m *model.Model) map[string]map[string][]string {
	namespaceNamePortMap := map[string]map[string][]string{}
	for _, listener := range m.HTTP {
		for _, route := range listener.Routes {
			mergeBackendsInNamespaceNamePortMap(route.Backends, namespaceNamePortMap)
			for _, mirror := range route.RequestMirrors {
				if mirror.Backend != nil {
					mergeBackendsInNamespaceNamePortMap([]model.Backend{*mirror.Backend}, namespaceNamePortMap)
				}
			}
		}
	}
	for _, listener := range m.TLS {
		for _, route := range listener.Routes {
			mergeBackendsInNamespaceNamePortMap(route.Backends, namespaceNamePortMap)
		}
	}
	for _, listener := range m.TCP {
		for _, route := range listener.Routes {
			mergeBackendsInNamespaceNamePortMap(route.Backends, namespaceNamePortMap)
		}
	}
	return namespaceNamePortMap
}

func mergeBackendsInNamespaceNamePortMap(backends []model.Backend, namespaceNamePortMap map[string]map[string][]string) {
	for _, backend := range backends {
		if backend.Port == nil {
			continue
		}
		namePortMap, exists := namespaceNamePortMap[backend.Namespace]
		if !exists {
			namePortMap = map[string][]string{}
			namespaceNamePortMap[backend.Namespace] = namePortMap
		}
		namePortMap[backend.Name] = slices.SortedUniqs(append(namePortMap[backend.Name], backend.Port.GetPort()))
	}
}

func getNamespaceNamePortsMapForHTTP(m *model.Model) map[string]map[string][]string {
	namespaceNamePortMap := map[string]map[string][]string{}
	for _, l := range m.HTTP {
		for _, r := range l.Routes {
			mergeBackendsInNamespaceNamePortMap(r.Backends, namespaceNamePortMap)
			for _, rm := range r.RequestMirrors {
				if rm.Backend == nil {
					continue
				}
				mergeBackendsInNamespaceNamePortMap([]model.Backend{*rm.Backend}, namespaceNamePortMap)
			}
		}
	}
	return namespaceNamePortMap
}

func getNamespaceNamePortsMapForTCP(m *model.Model) map[string]map[string][]string {
	namespaceNamePortMap := map[string]map[string][]string{}
	for _, listener := range m.TCP {
		for _, route := range listener.Routes {
			mergeBackendsInNamespaceNamePortMap(route.Backends, namespaceNamePortMap)
		}
	}
	return namespaceNamePortMap
}

func (i *defaultTranslator) getTCPRouteListeners(m *model.Model) []dolphinv1.XDSResource {
	var res []dolphinv1.XDSResource
	for _, listener := range m.TCP {
		cluster := ""
		for _, route := range listener.Routes {
			if len(route.Backends) == 0 {
				continue
			}
			backend := route.Backends[0]
			if backend.Port != nil {
				cluster = getClusterName(backend.Namespace, backend.Name, backend.Port.GetPort())
			}
			break
		}
		if cluster == "" {
			continue
		}
		l, err := NewTCPListenerWithDefaults("tcp-listener", listener.Port, cluster)
		if err == nil {
			res = append(res, l)
		}
	}
	return res
}

func toAny(msg proto.Message) *anypb.Any {
	a, err := anypb.New(msg)
	if err != nil {
		return nil
	}
	return a
}
