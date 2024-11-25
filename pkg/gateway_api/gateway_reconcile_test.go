package gateway_api

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
)

var gwFixture = []client.Object{

	// a valid gatewayClass with ObjectMeta, Spec

	// valid TLSRoute gateway

	// gateway with non-existent gateway class
	&gatewayv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayv1.GroupName,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-with-non-existent-gateway-class",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "non-existent-gateway-class",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Hostname: model.AddressOf[gatewayv1.Hostname]("*.dolphin.io"),
					Protocol: "http",
				},
			},
		},
	},
}

func Test_gatewayReconciler_Reconcile(t *testing.T) {
	// use fake build a fake client
	c := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(gwFixture...).
		WithStatusSubresource(&gatewayv1.Gateway{}).
		Build()
	// build a gatewayReconciler using the client
	r := gatewayReconciler{Client: c}

	t.Run("non-existent gateway class", func(t *testing.T) {
		key := client.ObjectKey{
			Namespace: "default",
			Name:      "gateway-with-non-existent-gateway-class",
		}
		r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: key,
		})
	})
}

func Test_isValidPemFromat(t *testing.T) {

}

func Test_sectionNameMatched(t *testing.T) {

}
