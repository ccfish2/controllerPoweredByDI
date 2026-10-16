package gateway_api

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
)

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      syscall.ECONNREFUSED,
			expected: true,
		},
		{
			name:     "connection reset",
			err:      syscall.ECONNRESET,
			expected: true,
		},
		{
			name:     "no route to host",
			err:      syscall.EHOSTUNREACH,
			expected: true,
		},
		{
			name:     "network unreachable",
			err:      syscall.ENETUNREACH,
			expected: true,
		},
		{
			name: "server timeout",
			err: k8serrors.NewServerTimeout(
				schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "gatewayclasses"},
				"get",
				5,
			),
			expected: true,
		},
		{
			name:     "service unavailable",
			err:      k8serrors.NewServiceUnavailable("API server is shutting down"),
			expected: true,
		},
		{
			name:     "too many requests",
			err:      k8serrors.NewTooManyRequests("rate limited", 5),
			expected: true,
		},
		{
			name:     "timeout",
			err:      k8serrors.NewTimeoutError("request timed out", 30),
			expected: true,
		},
		{
			name: "not found - permanent error",
			err: k8serrors.NewNotFound(
				schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "gatewayclasses"},
				"cilium",
			),
			expected: false,
		},
		{
			name:     "generic error - not transient",
			err:      errors.New("some random error"),
			expected: false,
		},
		{
			name:     "connection refused text is not a syscall error",
			err:      errors.New("dial tcp: connect: " + syscall.ECONNREFUSED.Error()),
			expected: false,
		},
		{
			name:     "wrapped connection refused syscall error",
			err:      fmt.Errorf("dial tcp: connect: %w", syscall.ECONNREFUSED),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isTransientError(tt.err), "isTransientError(%v)", tt.err)
		})
	}
}

func TestInitgatewayAPIController(t *testing.T) {
	tests := []struct {
		name          string
		configEnabled bool
		preconditions *gatewayAPIPreconditions
		client        client.Clientset
	}{
		{
			name: "Gateway API disabled",
		},
		{
			name:          "preconditions unavailable",
			configEnabled: true,
		},
		{
			name:          "preconditions failed",
			configEnabled: true,
			preconditions: &gatewayAPIPreconditions{Enabled: false},
		},
		{
			name:          "required CRDs missing",
			configEnabled: true,
			preconditions: &gatewayAPIPreconditions{Enabled: true},
			client:        newFakeClientset(t, helpers.RequiredGVKs[0].Kind, true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := initGatewayAPIController(gatewayAPIParams{
				Logger:        logrus.New(),
				K8sClient:     tt.client,
				Config:        gatewayApiConfig{EnableGatewayAPI: tt.configEnabled},
				Preconditions: tt.preconditions,
			})

			require.NoError(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestNewGatewayAPIPreconditionsDisabled(t *testing.T) {
	got, err := newGatewayAPIPreconditions(preconditionParams{
		GatewayApiConfig: gatewayApiConfig{EnableGatewayAPI: false},
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Enabled)
}

func TestRegisterReconciler(t *testing.T) {
	got, err := registerReconcilers(nil, "dolphin-secrets", 30, nil, nil)

	require.ErrorContains(t, err, "controller-runtime manager is nil")
	assert.Nil(t, got)
}

func TestCheckRequiredCRDs(t *testing.T) {
	tests := []struct {
		name        string
		missingKind string
		served      bool
		disable     bool
		wantError   string
	}{
		{
			name:   "all required CRDs are served",
			served: true,
		},
		{
			name:      "disabled client skips CRD checks",
			disable:   true,
			wantError: "",
		},
		{
			name:        "missing required CRD",
			missingKind: helpers.RequiredGVKs[0].Kind,
			served:      true,
			wantError:   "not found",
		},
		{
			name:      "required CRD version is not served",
			served:    false,
			wantError: "does not serve version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := newFakeClientset(t, tt.missingKind, tt.served)
			if tt.disable {
				fakeClient.Disable()
			}

			err := checkRequiredCRDs(t.Context(), fakeClient)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

type testGatewayAPIManager struct {
	ctrlmanager.Manager
	client ctrlclient.Client
}

func (m testGatewayAPIManager) GetClient() ctrlclient.Client {
	return m.client
}

func TestRegisterSecretSync(t *testing.T) {
	params := gatewayAPIParams{
		Logger: logrus.New(),
		Config: gatewayApiConfig{
			EnableGatewayAPI:            true,
			EnableGatewayAPISecretsSync: true,
			GatewayAPISecretsNamespace:  "gateway-secrets",
		},
		Preconditions: &gatewayAPIPreconditions{Enabled: true},
		K8sClient:     newFakeClientset(t, "", true),
		CtrlRuntimeManager: testGatewayAPIManager{
			client: ctrlclientfake.NewClientBuilder().Build(),
		},
	}

	registration := registerSecretSync(params).SecretSyncRegistration
	require.NotNil(t, registration)
	assert.IsType(t, &gatewayv1.Gateway{}, registration.RefObject)
	assert.NotNil(t, registration.RefObjectEnqueueFunc)
	assert.NotNil(t, registration.RefObjectCheckFunc)
	assert.Equal(t, "gateway-secrets", registration.SecretsNamespace)

	params.Config.EnableGatewayAPISecretsSync = false
	assert.Nil(t, registerSecretSync(params).SecretSyncRegistration)

	params.Config.EnableGatewayAPISecretsSync = true
	params.Preconditions.Enabled = false
	assert.Nil(t, registerSecretSync(params).SecretSyncRegistration)
}

func newFakeClientset(t *testing.T, missingKind string, served bool) client.Clientset {
	t.Helper()

	fakeClient, clientset := client.NewFakeClientset()
	for _, gvk := range helpers.RequiredGVKs {
		if gvk.Kind == missingKind {
			continue
		}

		crdName := fmt.Sprintf("%s.%s", gvk.Kind, gvk.Group)
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: crdName},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: gvk.Group,
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Plural: gvk.Kind,
					Kind:   gvk.Kind,
				},
				Scope: apiextensionsv1.NamespaceScoped,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name:    gvk.Version,
					Served:  served,
					Storage: true,
				}},
			},
		}
		_, err := fakeClient.APIExtFakeClientset.ApiextensionsV1().CustomResourceDefinitions().Create(
			t.Context(), crd, metav1.CreateOptions{},
		)
		require.NoError(t, err)
	}

	return clientset
}

func TestCheckCRD(t *testing.T) {
	gvk := helpers.RequiredGVKs[0]
	clientset := newFakeClientset(t, "", true)
	require.NoError(t, checkCRD(t.Context(), clientset, gvk))

	unservedClient := newFakeClientset(t, "", false)
	err := checkCRD(t.Context(), unservedClient, gvk)
	require.ErrorContains(t, err, "does not have version")
}
