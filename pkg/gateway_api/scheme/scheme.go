package scheme

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func AddToScheme(s *runtime.Scheme) error {
	for gv, addToScheme := range map[fmt.Stringer]func(*runtime.Scheme) error{
		gatewayv1.GroupVersion:       gatewayv1.AddToScheme,
		gatewayv1beta1.GroupVersion:  gatewayv1beta1.AddToScheme,
		gatewayv1alpha2.GroupVersion: gatewayv1alpha2.AddToScheme,
	} {
		if err := addToScheme(s); err != nil {
			return fmt.Errorf("failed to add types from %s to scheme: %w", gv, err)
		}
	}
	return nil
}
