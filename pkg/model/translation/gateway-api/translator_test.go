package gatewayapi

import (
	"fmt"
	"testing"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_Translator_Translate(t *testing.T) {
	type args struct {
		m model.Model
	}

	tests := []struct {
		name string
		args args
		want dolphinv1.DolphinEnvoyConfig
	}{{
		"test case one",
		args{
			m: model.Model{
				HTTP: basicHTTPListener,
			},
		},
		basicHTTPListenerEnvoyConfig,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator("", 50)
			got, _, _, err := translator.Translate(&tt.args.m)
			assert.Nil(t, err)
			assert.Equal(t, tt.want, got)
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
