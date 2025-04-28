package translation

import (
	"cmp"
	"fmt"
	goslices "slices"
	"sort"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
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
}

var _ Translator = (*defaultTranslator)(nil)

func NewTranslator(ns, nspace, secretsns string, enforcehttps bool, useproxyprotocl bool, hostNamesSuffixMatch bool, idleTimeoutSeconds int) Translator {
	return &defaultTranslator{
		name:                ns,
		namespace:           nspace,
		secretsNamespace:    secretsns,
		enforceHTTPs:        enforcehttps,
		useProxyProtocol:    useproxyprotocl,
		hostNameSuffixMatch: hostNamesSuffixMatch,
		idleTimeoutSeconds:  idleTimeoutSeconds,
	}
}

func (d *defaultTranslator) Translate(m *model.Model) (*dolphinv1.DolphinEnvoyConfig, *v1.Service, *v1.Endpoints, error) {
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
	return []*dolphinv1.ServiceListener{
		{
			Name:      i.name,
			Namespace: i.namespace,
		},
	}
}

func (i *defaultTranslator) getResources(m *model.Model) []dolphinv1.XDSResource {
	var res []dolphinv1.XDSResource
	res = append(res, i.getHTTPRouteListener(m)...)
	res = append(res, i.getTLSRouteListener(m)...)
	res = append(res, i.getEnvoyHTTPRouteConfiguration(m)...)
	res = append(res, i.getClusters(m)...)
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
	for ns, v := range getNamespaceNamePortsMapForTLS(m) {
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
	for _, t := range m.HTTP {
		for _, l := range t.Routes {
			for _, be := range l.Backends {
				namePortMap, exist := namespaceNamePortMap[be.Name]
				if exist {
					namePortMap[be.Name] = slices.SortedUniqs(append((namePortMap[be.Name]), be.Port.GetPort()))
				} else {
					namePortMap = map[string][]string{
						be.Name: {be.Port.GetPort()},
					}
				}
				mergeBackendsInNamespaceNamePortMap(l.Backends, namespaceNamePortMap)
			}

			for _, rm := range l.RequestMirrors {
				mergeBackendsInNamespaceNamePortMap([]model.Backend{*rm.Backend}, namespaceNamePortMap)
			}
		}
	}

	for _, l := range m.TLS {
		for _, r := range l.Routes {
			mergeBackendsInNamespaceNamePortMap(r.Backends, namespaceNamePortMap)
		}

	}
	return namespaceNamePortMap
}

func mergeBackendsInNamespaceNamePortMap(backends []model.Backend, namespaceNamePortMap map[string]map[string][]string) {
	for _, be := range backends {
		nameportMap, exist := namespaceNamePortMap[be.Name]
		if exist {
			nameportMap[be.Name] = slices.SortedUniqs(append((nameportMap[be.Name]), be.Port.GetPort()))
		} else {
			nameportMap = map[string][]string{
				be.Name: {be.Port.GetPort()},
			}
		}
		namespaceNamePortMap[be.Name] = nameportMap
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

func getNamespaceNamePortsMapForTLS(m *model.Model) map[string]map[string][]string {
	namespaceNamePortMap := map[string]map[string][]string{}
	for _, l := range m.TLS {
		for _, r := range l.Routes {
			mergeBackendsInNamespaceNamePortMap(r.Backends, namespaceNamePortMap)
		}
	}
	return namespaceNamePortMap
}

func toAny(msg proto.Message) *anypb.Any {
	a, err := anypb.New(msg)
	if err != nil {
		return nil
	}
	return a
}
