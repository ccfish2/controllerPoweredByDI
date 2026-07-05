package ingress

import (
	"context"
	"io"
	"testing"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8sApisErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

const (
	testDolphinNamespace                = "dolphin"
	testEnforceHTTPS                    = true
	testUseProxyProtocol                = true
	testDolphinSecretsNamespace         = "dolphin-secrets"
	testDefaultLoadbalancingServiceName = "dolphin-ingress"
	testDefaultSecretNamespace          = ""
	testDefaultSecretName               = ""
	testDefaultTimeout                  = 60
)

func TestReconcile(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	t.Run("Reconfile of dolphin ingress if there is not explicit loadbalancing creating default laodblancing if they don't exit", func(t *testing.T) {
		// create a fakeclient using Builder with scheme, watching objects ingress using default backend
		// newIngressReconcile
		// do the reconcile
		// expect no err, notnil res
		// this should trigger dolphin envoy config
		fakeclient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(
			&networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "test",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: model.AddressOf("dolphin"),
					DefaultBackend:   defaultBackend(),
				},
			},
		).Build()

		ingr := newIngressReconciler(logger, fakeclient, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)
		assert.NotNil(t, ingr)

		res, err := ingr.Reconcile(context.Background(), reconcile.Request{
			types.NamespacedName{Namespace: "test", Name: "test"}})
		assert.Nil(t, err)
		assert.NotNil(t, res)

		err = fakeclient.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test"}, &corev1.Service{})
		require.True(t, k8sApisErrors.IsNotFound(err), "Service should not be created")

		err = fakeclient.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test"}, &corev1.Endpoints{})
		require.NoError(t, err, "dedicated loadbalcner service endpoints should  be created")

		err = fakeclient.Get(context.Background(), types.NamespacedName{Namespace: "test", Name: "dolphin-ingress-test"}, &dolphinv1.DolphinEnvoyConfig{})
		require.NoError(t, err, "dedicated envoyconfig should exist")
		expect := dolphinv1.DolphinEnvoyConfig{}
		err = fakeclient.Get(context.Background(), types.NamespacedName{Namespace: testDolphinNamespace, Name: testDefaultLoadbalancingServiceName}, &expect)
		require.NoError(t, err, "attempt to cleanup shared envoy config ")
		require.Empty(t, expect.Spec.Resources)
	})

	t.Run("Controller will reconcile ingress if it does not have specific IngressClassName if the default ingress class is dolphin", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(
			&networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "test",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: model.AddressOf("dolphin"),
					DefaultBackend:   defaultBackend(),
				},
			},
			&networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dolphin",
					Annotations: map[string]string{
						"ingressclass.kubernetes.io/is-default-class": "true",
					},
				},
			},
		).Build()

		ingr := newIngressReconciler(logger, fakeClient, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)
		assert.NotNil(t, ingr)

		res, err := ingr.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, res)

		dec := dolphinv1.DolphinEnvoyConfig{}
		err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "dolphin-ingress-test"}, &dec)
		assert.Nil(t, err)
		assert.NotEmpty(t, dec, "this ingress should be able to get reconciled")

	})

	t.Run("Controller will not reconcile ingress if it does not have specific IngressClassName if the default ingress class isnot dolphin", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(
			&networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "test",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: model.AddressOf("dolphin"),
					DefaultBackend:   defaultBackend(),
				},
			},
			&networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dolphin",
					Annotations: map[string]string{
						"ingressclass.kubernetes.io/is-default-class": "false",
					},
				},
			},
		).Build()

		ingr := newIngressReconciler(logger, fakeClient, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDolphinSecretsNamespace, []string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)
		assert.NotNil(t, ingr)

		res, err := ingr.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "test",
				Name:      "test",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, res)

		dec := dolphinv1.DolphinEnvoyConfig{}
		err = fakeClient.Get(context.Background(), client.ObjectKey{Namespace: "test", Name: "dolphin-ingress-test"}, &dec)
		assert.Nil(t, err)
		assert.NotEmpty(t, dec, "this ingress should be not able to get reconciled since dolphin is not the default ingress class")
	})

	t.Run("ingress annotated with shared ingress would be reconciled", func(t *testing.T) {
		fakeCli := fake.NewClientBuilder().
			WithScheme(testScheme()).
			WithObjects().
			Build()

		ingressReconcile := newIngressReconciler(logger, fakeCli, testDolphinNamespace, testEnforceHTTPS, testUseProxyProtocol, testDefaultSecretNamespace,
			[]string{}, testDefaultLoadbalancingServiceName, "dedicated", testDefaultSecretNamespace, testDefaultSecretName, testDefaultTimeout)

		result, err := ingressReconcile.Reconcile(context.Background(), reconcile.Request{types.NamespacedName{"test", "test"}})
		require.NoError(t, err)
		assert.NotNil(t, result)

		// ensure dolphinenvoyconfig, coreservice loadbalancer is created successfully

	})

	t.Run("If create operations fail due to namespace termination, no error should be reported",
		func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(testScheme()).
				WithObjects(
					&networkingv1.Ingress{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "test",
							Name:      "test",
						},
						Spec: networkingv1.IngressSpec{
							IngressClassName: model.AddressOf("dolphin"),
							DefaultBackend:   defaultBackend(),
						},
					},
				).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						return &k8sApisErrors.StatusError{
							ErrStatus: metav1.Status{
								Message: "unable to create new content in namespace test becauseit is being terminated",
								Reason:  metav1.StatusReasonForbidden,
								Details: &metav1.StatusDetails{
									Causes: []metav1.StatusCause{
										{
											Type: corev1.NamespaceTerminatingCause,
										},
									},
								},
							},
						}
					},
				}).Build()

			reconciler := newIngressReconciler(logger,
				fakeClient, "dolphin", true, true,
				"dolphin-secrets", []string{}, "dolphin-ingress", "dedicated",
				"", "", 60)

			result, err := reconciler.Reconcile(context.Background(),
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: "test",
						Name:      "test",
					},
				})

			require.NoError(t, err)
			require.NotNil(t, result)

			err = fakeClient.Get(context.Background(), types.NamespacedName{}, &corev1.Service{})
			require.True(t, k8sApisErrors.IsNotFound(err), "Service should not be created")

		})
}

func defaultBackend() *networkingv1.IngressBackend {
	return &networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: "test",
			Port: networkingv1.ServiceBackendPort{
				Number: 8080,
			},
		},
	}
}
