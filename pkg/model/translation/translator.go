package translation

import (
	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"

	// dolphin
	"github.com/ccfish2/infra/pkg/k8s"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/slices"
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

func NewTranslator(ns, nspace, secretsns string, enforcehttps bool, useproxyprotocl bool, hostNamesSuffixMatch bool, idleTimeoutSeconds int) defaultTranslator {
	return defaultTranslator{
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

	return res
}

func (d *defaultTranslator) getServices(m *model.Model) []*dolphinv1.ServiceListener {
	var res []*dolphinv1.ServiceListener
	return res
}

func (d *defaultTranslator) getResources(_ *model.Model) []dolphinv1.XDSResource {
	var res []dolphinv1.XDSResource

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
