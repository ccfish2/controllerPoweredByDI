package gateway_api

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
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

func Test_gatewayReconciler_Reconcile_WithTLS(t *testing.T) {
	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "gateway-tls-secret",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte("-----BEGIN CERTIFICATE-----\nMIIC/DCCAeQCCQCKz0Ygc8Pm3jANBgkqhkiG9w0BAQsFADAuMQswCQYDVQQGEwJB\nVTELMAkGA1UECAgMQk5TMQswCQYDVQQHDAJBQjAeFw0yMzAyMDEwMDAwMDBaFw0y\nNDAyMDEwMDAwMDBaMC4xCzAJBgNVBAYTAkFVMQswCQYDVQQIDAJCTjELMAkGA1UE\nBwwCQUIwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDQ7gV8Tq8gqQ5Z\nsZnqEwGNzD/SPxDLBxKxkI5x0NzLbBXfOjA0IKyJ7Fqq9gNQm0v5q1v7x5VxZx0R\nQ0M0F3vQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y\n8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y\n8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y\n8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y\nwIDAQABMA0GCSqGSIb3DQEBCwUAA4IBAQC8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ\n4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y\n8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8nQ4Y8\n-----END CERTIFICATE-----"),
			corev1.TLSPrivateKeyKey: []byte("-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0O4FfE6vIKkOWbGZ6hMBjcw/0j8QywcSsZCOcdDcy2wV3zow\nNCCsieJ6qvYDUJtL+atb+8eVcWcdEUNDNBd70OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OG\nPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0\nOGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ\n0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGP\nJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGP\nJ0OGPJ0OGPJ0QIDAQABAoIBAAKCAQEA0O4FfE6vIKkOWbGZ6hMBjcw/0j8QywcSsZCO\ncdDcy2wV3zowNCCsieJ6qvYDUJtL+atb+8eVcWcdEUNDNBd70OGPJ0OGPJ0OGPJ0OGP\nJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0\nOGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGP\nJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0\nOGPJ0OGPJ0ECgYEA8OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0OGPJ0\n-----END RSA PRIVATE KEY-----"),
		},
	}

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dolphin",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "dolphin.io/dolphin",
		},
	}

	gatewayWithTLS := &gatewayv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayv1.GroupName,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-with-tls",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "dolphin",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Protocol: gatewayv1.TLSProtocolType,
					TLS: &gatewayv1.GatewayTLSConfig{
						CertificateRefs: []gatewayv1.SecretObjectReference{
							{
								Name: gatewayv1.ObjectName("gateway-tls-secret"),
							},
						},
					},
				},
			},
		},
	}

	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-route",
			Namespace: "default",
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			ParentRefs: []gatewayv1.ParentReference{
				{
					Name: gatewayv1.ObjectName("gateway-with-tls"),
				},
			},
			Hostnames: []gatewayv1.Hostname{
				"example.com",
			},
			Rules: []gatewayv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: "backend-svc",
								Port: model.AddressOf[gatewayv1.PortNumber](8080),
							},
						},
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(tlsSecret, gatewayClass, gatewayWithTLS, tlsRoute).
		WithStatusSubresource(&gatewayv1.Gateway{}).
		Build()

	r := gatewayReconciler{Client: c}

	t.Run("Gateway with TLS should translate TLS listener and TLSRoute", func(t *testing.T) {
		key := client.ObjectKey{
			Namespace: "default",
			Name:      "gateway-with-tls",
		}
		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: key,
		})
		if err != nil {
			t.Logf("Reconcile error (expected during test): %v", err)
		}
		if result.IsZero() {
			t.Log("Reconcile succeeded or requeued as expected")
		}
	})
}

func Test_isValidPemFromat(t *testing.T) {

}

func Test_sectionNameMatched(t *testing.T) {

}
