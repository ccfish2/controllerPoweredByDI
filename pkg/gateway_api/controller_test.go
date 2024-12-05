package gateway_api

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/stretchr/testify/assert"
)

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(dolphinv1.AddToScheme(scheme))

	registerGatewayAPITypesToScheme(scheme)
	return scheme
}

var ctrlTestFixture = []client.Object{

	&gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dolphin",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "io.dolphin/gateway-controller",
		}},

	&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-secret",
			Namespace: "default",
		},
		StringData: map[string]string{
			"tls.crt": "crt",
			"tls.key": "key",
		},
		Type: corev1.SecretTypeTLS,
	},

	// Valid gateway
	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw-with-tls",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Hostname: model.AddressOf[gatewayv1.Hostname]("example.com"),
					TLS: &gatewayv1.GatewayTLSConfig{
						CertificateRefs: []gatewayv1.SecretObjectReference{
							{Name: "gateway-secret"},
						},
					},
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-with-tls",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     80,
					Hostname: model.AddressOf[gatewayv1.Hostname]("example2.com"),
					TLS: &gatewayv1.GatewayTLSConfig{
						CertificateRefs: []gatewayv1.SecretObjectReference{},
					},
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-without-tls",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name: "https",
					Port: 80,
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-with-tls-route",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "tls",
					Port:     443,
					Protocol: gatewayv1.TLSProtocolType,
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-for-tls-rOUTE",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Hostname: model.AddressOf[gatewayv1.Hostname]("example3.com"),
					AllowedRoutes: &gatewayv1.AllowedRoutes{
						Namespaces: &gatewayv1.RouteNamespaces{
							From: model.AddressOf(gatewayv1.NamespacesFromSame),
						},
					},
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-for-tls-rOUTE",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Hostname: model.AddressOf[gatewayv1.Hostname]("example3.com"),
					TLS:      &gatewayv1.GatewayTLSConfig{},
					AllowedRoutes: &gatewayv1.AllowedRoutes{
						Namespaces: &gatewayv1.RouteNamespaces{
							From: model.AddressOf(gatewayv1.NamespacesFromAll),
						},
					},
				},
			},
		},
	},

	&gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-gw-for-tls-rOUTE",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Hostname: model.AddressOf[gatewayv1.Hostname]("example3.com"),
					TLS:      &gatewayv1.GatewayTLSConfig{},
					AllowedRoutes: &gatewayv1.AllowedRoutes{
						Namespaces: &gatewayv1.RouteNamespaces{
							From: model.AddressOf(gatewayv1.NamespacesFromAll),
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"gateway": "allowed",
								},
							},
						},
					},
				},
			},
		},
	},
}

var namespaceFixture = []client.Object{}

func Test_hasMathingController(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(ctrlTestFixture...).Build()
	fn := hasMatchingController(context.TODO(), c, "io.dolphin/gateway-controller")

	t.Run("invalid object", func(t *testing.T) {
		res := fn(&corev1.Pod{})
		assert.Equal(t, res, false)
	})

	t.Run("matched controllers", func(t *testing.T) {
		res := fn(&gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "dolphin",
			},
		})
		assert.Equal(t, res, true)
	})

	t.Run("controller does not match", func(t *testing.T) {
		res := fn(&gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "does not exist",
			},
		})
		assert.Equal(t, res, false)
	})
}
