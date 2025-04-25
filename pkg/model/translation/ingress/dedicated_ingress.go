package ingress

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	corev1 "k8s.io/api/core/v1"
)

const (
	dolphinIngressPrefix   = "dolphin-ingress"
	dolphinIngressLabelKey = "dolphin.io/ingress"
)

var _ translation.Translator = (*DedicatedIngressTranslator)(nil)

type DedicatedIngressTranslator struct {
	secretsNamespace   string
	enforceHTTPs       bool
	useProxyProtocol   bool
	idleTimeoutSeconds int
}

func NewDedicatedIngressTranslator(secretsNamespace string, enforceHTTPs bool, useProxyProtocol bool, idleTimeoutSeconds int) *DedicatedIngressTranslator {
	return &DedicatedIngressTranslator{
		secretsNamespace:   secretsNamespace,
		enforceHTTPs:       enforceHTTPs,
		useProxyProtocol:   useProxyProtocol,
		idleTimeoutSeconds: idleTimeoutSeconds,
	}
}

func (d *DedicatedIngressTranslator) Translate(m *model.Model) (*dolphinv1.DolphinEnvoyConfig, *corev1.Service, *corev1.Endpoints, error) {
	if m == nil || (len(m.HTTP) == 0 && len(m.TLS) == 0) {
		return nil, nil, nil, fmt.Errorf("model source can't be empty")
	}

	var name string
	var namespace string
	var sourceResource model.FullyQualifiedResource
	var modelService *model.Service
	var cecName string

	if len(m.HTTP) == 0 {
		name = fmt.Sprintf("%s-%s", dolphinIngressPrefix, m.TLS[0].Sources[0].Name)
		namespace = m.TLS[0].Sources[0].Namespace
		sourceResource = m.TLS[0].Sources[0]
		modelService = m.TLS[0].Service
		cecName = fmt.Sprintf("%s-%s-%s", dolphinIngressPrefix, namespace, m.TLS[0].Sources[0].Name)
	} else {
		name = fmt.Sprintf("%s-%s", dolphinIngressPrefix, m.HTTP[0].Sources[0].Name)
		namespace = m.HTTP[0].Sources[0].Namespace
		sourceResource = m.HTTP[0].Sources[0]
		modelService = m.HTTP[0].Service
		cecName = fmt.Sprintf("%s-%s-%s", dolphinIngressPrefix, namespace, m.HTTP[0].Sources[0].Name)
	}

	// The logic is same as what we have with default translator, but with a different model
	// (i.e. the HTTP listeners are just belonged to one Ingress resource).
	translator := translation.NewTranslator(name, namespace, d.secretsNamespace, d.enforceHTTPs, d.useProxyProtocol, false, d.idleTimeoutSeconds)
	cec, _, _, err := translator.Translate(m)
	if err != nil {
		return nil, nil, nil, err
	}

	// Set the name to avoid any breaking change during upgrade.
	cec.Name = cecName
	return cec, getService(sourceResource, modelService), getEndpoints(sourceResource), err
}

func getService(resource model.FullyQualifiedResource, service *model.Service) *corev1.Service {
	serviceType := corev1.ServiceTypeLoadBalancer
	ports := []corev1.ServicePort{
		{
			Name:     "http",
			Protocol: "TCP",
			Port:     80,
		},
		{
			Name:     "https",
			Protocol: "TCP",
			Port:     443,
		},
	}

	if service != nil {
		switch service.Type {
		case string(corev1.ServiceTypeNodePort):
			serviceType = corev1.ServiceTypeNodePort
			if service.InsecureNodePort != nil {
				ports[0].NodePort = int32(*service.InsecureNodePort)
			}
			if service.SecureNodePort != nil {
				ports[1].NodePort = int32(*service.SecureNodePort)
			}
		case string(corev1.ServiceTypeLoadBalancer):
			// Do nothing as the port number is allocated by the cloud provider.
		default:
			log.WithField(logfields.ServiceType, service.Type).
				Warn("only LoadBalancer and NodePort are supported. Defaulting to LoadBalancer")
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", dolphinIngressPrefix, resource.Name),
			Namespace: resource.Namespace,
			Labels:    map[string]string{dolphinIngressLabelKey: "true"},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "Ingress",
					Name:       resource.Name,
					UID:        types.UID(resource.UID),
					Controller: model.AddressOf(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  serviceType,
			Ports: ports,
		},
	}
}

func getEndpoints(resource model.FullyQualifiedResource) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", dolphinIngressPrefix, resource.Name),
			Namespace: resource.Namespace,
			Labels:    map[string]string{dolphinIngressLabelKey: "true"},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "Ingress",
					Name:       resource.Name,
					UID:        types.UID(resource.UID),
					Controller: model.AddressOf(true),
				},
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				// This dummy endpoint is required as agent refuses to push service entry
				// to the lb map when the service has no backends.
				// Related github issue https://github.com/dolphin/dolphin/issues/19262
				Addresses: []corev1.EndpointAddress{{IP: "192.192.192.192"}}, // dummy
				Ports:     []corev1.EndpointPort{{Port: 9999}},               // dummy
			},
		},
	}
}
