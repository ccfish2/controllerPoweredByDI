package scheme

import (
	"fmt"

	helpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

	for _, optionalKind := range helpers.AllOptionalKinds {
		addToSchema[optionalKind] = func(s *runtime.Scheme) error {
			s.AddKnownTypes(
				optionalKind.GroupVersion(),
				helpers.GetConcreteObject(optionalKind),
				helpers.GetConcreteListObject(optionalKind),
			)
			metav1.AddToGroupVersion(s, optionalKind.GroupVersion())
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

func TestScheme(optionalKinds []schema.GroupVersionKind) *runtime.Scheme {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dolphinv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	AddToScheme(scheme)

	return scheme
}
