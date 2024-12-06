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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
			Name: "dolphin",
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
			Name:      "another-gw-without-tls-route",
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
			Name:      "gateway-from-same-namespaces",
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
			Name: "gateway-from-all-namespaces",
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
			Name:      "another-gw-for-tls-with-selector",
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

var namespaceFixture = []client.Object{

	&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	},

	&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "another-namespace",
		},
	},

	&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gw-allowed-namespace",
			Labels: map[string]string{
				"gateway": "allowed",
			},
		},
	},

	&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gw-notallowed-namespace",
			Labels: map[string]string{
				"gateway": "disallowed",
			},
		},
	},
}

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

func Test_OnlyStatusChange(t *testing.T) {
	failureFunc := predicate.Funcs{
		CreateFunc: func(tce event.TypedCreateEvent[client.Object]) bool {
			t.Fail()
			return false
		},
		DeleteFunc: func(tde event.TypedDeleteEvent[client.Object]) bool {
			t.Fail()
			return false
		},
		UpdateFunc: func(tue event.TypedUpdateEvent[client.Object]) bool {
			t.Fail()
			return false
		},
		GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool {
			t.Fail()
			return false
		},
	}
	f := failureFunc
	f.UpdateFunc = onlyStatusChanged().Update

	type args struct {
		evt event.UpdateEvent
	}

	tests := []struct {
		name     string
		arg      args
		expected bool
	}{
		{
			"unsupoorted kind",
			args{
				event.UpdateEvent{
					ObjectOld: &corev1.Pod{},
					ObjectNew: &corev1.Pod{},
				},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := f.Update(tt.arg.evt)
			assert.Equal(t, tt.expected, res)
		})
	}
}

func Test_SelectGWForNamespace(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(namespaceFixture...).Build()
	type args struct {
		namespacce string
	}

	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "from-same-and-all-namespace",
			args: args{namespacce: "default"},
			want: []string{"gateway-from-all-namespaces", "gateway-from-same-namespaces"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gwlist := getGatewaysForNamespace(context.Background(), c, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.args.namespacce,
				},
			})

			res := make([]string, len(gwlist))
			for _, gw := range gwlist {
				res = append(res, gw.Name)
			}
			assert.Equal(t, tt.args.namespacce, res)
		})
	}
}
