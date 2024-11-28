package gatewayapi

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	dolphinGatewayPrefix = "dolphin-gateway-"
	owningGatewayLabel   = "io.dolphin.gateway/owning-gateway"
)

type translator struct {
	SecretNameSpace    string
	idleTimeoutSeconds int
}

// Translate implements Translator.
func (t *translator) Translate(m *model.Model) (*dolphinv1.DolphinEnvoyConfig, *v1.Service, *v1.Endpoints, error) {
	listenrs := m.GetListerners()
	if len(listenrs) == 0 || len(listenrs[0].GetSources()) == 0 {
		return nil, nil, nil, fmt.Errorf("no listeners")
	}
	var source *model.FullyQualifiedResource
	var ports []uint32
	for _, l := range listenrs {
		source = &l.GetSources()[0]
		ports = append(ports, l.GetPort())
	}
	if source == nil || source.Name == "" {
		return nil, nil, nil, fmt.Errorf("MODEL source name could not be empty")
	}
	trans := translation.NewTranslator(dolphinGatewayPrefix+source.Name, source.Namespace, t.SecretNameSpace, false, false, true, t.idleTimeoutSeconds)
	dec, _, _, err := trans.Translate(m)
	if err != nil {
		return nil, nil, nil, err
	}
	dec.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: gatewayv1beta1.GroupVersion.String(),
			Kind:       source.Kind,
			Name:       source.Name,
			UID:        types.UID(source.UID),
			Controller: model.AddressOf(true),
		},
	}

	allLabels, allAnnotations := map[string]string{}, map[string]string{}
	for _, l := range listenrs {
		allAnnotations = mergeMap(allAnnotations, l.GetAnnotations())
		allLabels = mergeMap(allLabels, l.GetLabels())
	}
	return dec, getService(source, ports, allLabels, allAnnotations), getEndpoints(*source), err
}

var _ translation.Translator = (*translator)(nil)

func NewTranslator(ns string, idle int) translator {
	return translator{
		ns,
		idle,
	}
}

func mergeMap(left, right map[string]string) map[string]string {
	if left == nil {
		return right
	}
	for key, value := range right {
		left[key] = value
	}
	return left
}

// compse gateway api laodbalance servce type
func getService(resource *model.FullyQualifiedResource, allPorts []uint32, labels, annotations map[string]string) *corev1.Service {
	uniqPorts := map[uint32]struct{}{}
	for _, p := range allPorts {
		uniqPorts[p] = struct{}{}
	}

	ports := make([]corev1.ServicePort, 0, len(uniqPorts))
	for p := range uniqPorts {
		ports = append(ports, corev1.ServicePort{
			Name:     fmt.Sprintf("port-%d", p),
			Port:     int32(p),
			Protocol: corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        model.Shorten(dolphinGatewayPrefix + resource.Name),
			Namespace:   resource.Namespace,
			Annotations: annotations,
			Labels:      mergeMap(map[string]string{owningGatewayLabel: model.Shorten(resource.Name)}, labels),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1beta1.GroupVersion.String(),
					Kind:       resource.Kind,
					Name:       resource.Name,
					UID:        types.UID(resource.UID),
					Controller: model.AddressOf(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: ports,
		},
	}
}

func getEndpoints(resource model.FullyQualifiedResource) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resource.Name,
			Namespace: resource.Namespace,
			Labels:    map[string]string{owningGatewayLabel: model.Shorten(resource.Name)},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gatewayv1beta1.GroupVersion.String(),
					Kind:       resource.Kind,
					Name:       resource.Name,
					UID:        types.UID(resource.UID),
					Controller: model.AddressOf(true),
				},
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{{IP: "192.192.192.192"}},
				Ports:     []corev1.EndpointPort{{Port: 9999}},
			},
		},
	}
}
