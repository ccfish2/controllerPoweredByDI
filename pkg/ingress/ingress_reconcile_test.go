package ingress

import (
	"context"
	"io"
	"testing"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/sirupsen/logrus"
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
)

func TestReconcile(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

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
