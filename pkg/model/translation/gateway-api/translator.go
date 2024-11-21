package gatewayapi

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
	"github.com/ccfish2/controller-powered-by-DI/pkg/model/translation"

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

func getService(resource *model.FullyQualifiedResource, allPorts []uint32, labels, annotations map[string]string) *corev1.Service {
	panic("")
}

func getEndpoints(resource model.FullyQualifiedResource) *corev1.Endpoints {
	panic("")
}
