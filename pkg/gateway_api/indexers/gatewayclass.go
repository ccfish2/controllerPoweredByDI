package indexers

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

// we do not apply parentRef part
// kind,group,name,namespace
func IndexGatewayClassByDolphinGatewayClassConfig(controllerName gatewayv1.GatewayController) client.IndexerFunc {
	return func(rawObj client.Object) []string {
		gwc, ok := rawObj.(*gatewayv1.GatewayClass)
		if !ok {
			return nil
		}

		if gwc.Spec.ControllerName != controllerName {
			return nil
		}

		if gwc.Spec.ParametersRef == nil ||
			gwc.Spec.ParametersRef.Group != dolphinv1.CustomResourceDefinitionGroup ||
			gwc.Spec.ParametersRef.Kind != dolphinv1.DECKindDefinition ||
			gwc.Spec.ParametersRef.Namespace == nil {
			return nil
		}

		return []string{types.NamespacedName{
			Namespace: string(*gwc.Spec.ParametersRef.Namespace),
			Name:      gwc.Spec.ParametersRef.Name,
		}.String()}
	}
}
