package scheme

import (
	"fmt"

	helpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func AddToScheme(scheme *runtime.Scheme) error {
	addToSchema := make(map[fmt.Stringer]func(s *runtime.Scheme) error)

	// Install all required GVKs.
	for _, gvk := range helpers.RequiredGVKs {
		addToSchema[gvk] = func(s *runtime.Scheme) error {
			s.AddKnownTypes(
				gvk.GroupVersion(),
				helpers.GetConcreteObject(gvk),
				helpers.GetConcreteListObject(gvk),
			)
			metav1.AddToGroupVersion(s, gvk.GroupVersion())
			return nil
		}
	}

	// We can also safely install the v1beta1 resources, as these are legacy
	// and also included in the Standard install
	addToSchema[gatewayv1beta1.GroupVersion] = gatewayv1beta1.Install

	for gv, f := range addToSchema {
		if err := f(scheme); err != nil {
			return fmt.Errorf("failed to add types from %s to scheme: %w", gv, err)
		}
	}
	return nil
}
