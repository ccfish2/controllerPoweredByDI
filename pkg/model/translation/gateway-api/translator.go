package gatewayapi

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	v1 "k8s.io/api/core/v1"
)

const (
	dolphinGatewayPrefix = "dolphin-gateway-"
	owningGatewayLabel   = "io.dolphin.gateway/owning-gateway"
)

type translator struct {
	SecretNameSpace    string
	idleTimeoutSeconds int
	enableIpv4         bool
	enableIpv6         bool
}

// Translate implements Translator.
func (t *translator) Translate(
	m *model.Model,
	dgccfg ...*dolphinv2alpha1.DolphinGatewayClassConfig,
) (*dolphinv1.DolphinEnvoyConfig, *v1.Service, *v1.Endpoints, error) {
	if m == nil {
		return nil, nil, nil, fmt.Errorf("model is nil")
	}

	var source *model.FullyQualifiedResource
	var ports []uint32
	allLabels := map[string]string{}
	allAnnotations := map[string]string{}

	addListener := func(
		sources []model.FullyQualifiedResource,
		port uint32,
		infra *model.Infrastructure,
	) {
		if source == nil && len(sources) > 0 && sources[0].Name != "" {
			src := sources[0]
			source = &src
		}
		ports = append(ports, port)

		if infra != nil {
			allLabels = mergeMap(allLabels, infra.Labels)
			allAnnotations = mergeMap(allAnnotations, infra.Annotations)
		}
	}

	for _, l := range m.HTTP {
		addListener(l.Sources, l.Port, l.Infrastructure)
	}
	for _, l := range m.TLS {
		addListener(l.Sources, l.Port, l.Infrastructure)
	}
	for _, l := range m.TCP {
		addListener(l.Sources, l.Port, l.Infrastructure)
	}

	if source == nil {
		return nil, nil, nil, fmt.Errorf("model source can't be empty")
	}

	var cfg *dolphinv2alpha1.DolphinGatewayClassConfig
	if len(dgccfg) > 0 {
		cfg = dgccfg[0]
	}

	trans := translation.NewTranslator(
		dolphinGatewayPrefix+source.Name,
		source.Namespace,
		t.SecretNameSpace,
		false,
		false,
		true,
		t.idleTimeoutSeconds,
		t.enableIpv4,
		t.enableIpv6,
	)
	dec, _, _, err := trans.Translate(m, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	dec.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: gatewayv1beta1.GroupVersion.String(),
		Kind:       source.Kind,
		Name:       source.Name,
		UID:        types.UID(source.UID),
		Controller: model.AddressOf(true),
	}}

	return dec,
		getService(source, ports, allLabels, allAnnotations, cfg),
		getEndpoints(*source),
		nil
}

var _ translation.Translator = (*translator)(nil)

func NewTranslator(ns string, idle int, enableIpv4 bool, enableIpv6 bool) translator {
	return translator{
		ns,
		idle,
		enableIpv4,
		enableIpv6,
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

func getService(
	resource *model.FullyQualifiedResource,
	allPorts []uint32,
	labels, annotations map[string]string,
	dgccfg *dolphinv2alpha1.DolphinGatewayClassConfig,
) *corev1.Service {
	uniquePorts := make(map[uint32]struct{}, len(allPorts))
	for _, p := range allPorts {
		uniquePorts[p] = struct{}{}
	}

	sortedPorts := make([]uint32, 0, len(uniquePorts))
	for p := range uniquePorts {
		sortedPorts = append(sortedPorts, p)
	}
	sort.Slice(sortedPorts, func(i, j int) bool {
		return sortedPorts[i] < sortedPorts[j]
	})

	ports := make([]corev1.ServicePort, 0, len(sortedPorts))
	for _, p := range sortedPorts {
		ports = append(ports, corev1.ServicePort{
			Name:       fmt.Sprintf("port-%d", p),
			Port:       int32(p),
			TargetPort: intstr.FromInt32(int32(p)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        model.Shorten(dolphinGatewayPrefix + resource.Name),
			Namespace:   resource.Namespace,
			Labels:      mergeMap(map[string]string{owningGatewayLabel: model.Shorten(resource.Name)}, labels),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: gatewayv1beta1.GroupVersion.String(),
				Kind:       resource.Kind,
				Name:       resource.Name,
				UID:        types.UID(resource.UID),
				Controller: model.AddressOf(true),
			}},
		},
		Spec: corev1.ServiceSpec{
			Type:  toServiceType(dgccfg),
			Ports: ports,
		},
	}
}

func toServiceType(dgccfg *dolphinv2alpha1.DolphinGatewayClassConfig) corev1.ServiceType {
	if dgccfg != nil && dgccfg.Spec.Service.Type == v1.ServiceTypeNodePort {
		return corev1.ServiceTypeNodePort
	}

	return corev1.ServiceTypeLoadBalancer
}

func getEndpoints(resource model.FullyQualifiedResource) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      model.Shorten(dolphinGatewayPrefix + resource.Name),
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
				Addresses: []corev1.EndpointAddress{{IP: "192.192.192.192"}}, // dummy
				Ports:     []corev1.EndpointPort{{Port: 9999}},               // dummy
			},
		},
	}
}
