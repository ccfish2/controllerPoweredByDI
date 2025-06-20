package gatewayapi

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8syaml "sigs.k8s.io/yaml"
)

func Test_Translator_Translate(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "basic_http_listener"},
		{name: "basic_tls_sni_listener"},
		{name: "conformance/httproute_simple_same_namespace"},
		{name: "conformance/httproute_backend_protocol_h_2_c"},
		{name: "conformance/httproute_cross_namespace"},
		{name: "conformance/httpexact_path_matching"},
		{name: "conformance/httproute_header_matching"},
		{name: "conformance/httproute_hostname_intersection"},
		{name: "conformance/httproute_listener_hostname_matching"},
		{name: "conformance/httproute_matching_across_routes"},
		{name: "conformance/httproute_matching"},
		{name: "conformance/httproute_method_matching"},
		{name: "conformance/httproute_query_param_matching"},
		{name: "conformance/httproute_request_header_modifier"},
		{name: "conformance/httproute_backend_refs_request_header_modifier"},
		{name: "conformance/httproute_request_redirect"},
		{name: "conformance/httproute_response_header_modifier"},
		{name: "conformance/httproute_backend_refs_response_header_modifier"},
		{name: "conformance/httproute_rewrite_host"},
		{name: "conformance/httproute_rewrite_path"},
		{name: "conformance/httproute_request_mirror"},
		{name: "conformance/httproute_request_redirect_with_multi_httplisteners"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trans := &translator{
				idleTimeoutSeconds: 60,
				SecretNameSpace:    "dolphin-secrets",
				enableIpv4:         true,
				enableIpv6:         true,
			}

			input := &model.Model{}
			readInput(t, fmt.Sprintf("testdata/%s/input.yaml", tt.name), input)
			output := &dolphinv1.DolphinEnvoyConfig{}
			readOutput(t, fmt.Sprintf("testdata/%s/cec-output.yaml", tt.name), output)

			cec, _, _, err := trans.Translate(input)

			require.Equal(t, tt.wantErr, err != nil, "Error mismatch")
			diffOutput := cmp.Diff(output, cec, protocmp.Transform())
			if len(diffOutput) != 0 {
				t.Errorf("DolphinEnvoyConfigs did not match:\n%s\n", diffOutput)
			}
		})
	}
}

func Test_GetService(t *testing.T) {
	type args struct {
		resource    *model.FullyQualifiedResource
		allports    []uint32
		labels      map[string]string
		annotations map[string]string
	}
	tests := []struct {
		name string
		args args
		want *corev1.Service
	}{{
		"one long test cases",
		args{
			resource: &model.FullyQualifiedResource{
				Name:      "long time running test cases",
				Namespace: "default",
				Version:   "v1",
				Kind:      "Gteway",
				UID:       "12345-5678",
			},
			allports: []uint32{80},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "",
				Namespace: "default",
				Labels: map[string]string{
					"": "",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: gatewayv1.GroupVersion.Version,
						Kind:       "Gateway",
						Name:       "test-long-time-gateway-services",
						UID:        types.UID(""),
					},
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     fmt.Sprintf("port-%d", 80),
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					},
				},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getService(tt.args.resource, tt.args.allports, tt.args.labels, tt.args.annotations)
		})
	}
}

func readInput(t *testing.T, file string, obj any) {
	inputYaml, err := os.ReadFile(file)
	require.NoError(t, err)

	require.NoError(t, k8syaml.Unmarshal(inputYaml, obj))
}

func readOutput(t *testing.T, file string, obj any) string {
	// unmarshal and marshal to prevent formatting diffs
	outputYaml, err := os.ReadFile(file)
	require.NoError(t, err)

	if strings.TrimSpace(string(outputYaml)) == "" {
		return strings.TrimSpace(string(outputYaml))
	}

	require.NoError(t, k8syaml.Unmarshal(outputYaml, obj))

	yamlText := toYaml(t, obj)

	return strings.TrimSpace(yamlText)
}

func toYaml(t *testing.T, obj any) string {
	yamlText, err := k8syaml.Marshal(obj)
	require.NoError(t, err)

	return strings.TrimSpace(string(yamlText))
}
