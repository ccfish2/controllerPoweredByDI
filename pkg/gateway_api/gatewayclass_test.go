package gateway_api

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_matchesController(t *testing.T) {
	tests := []struct {
		name   string
		object client.Object
		expect bool
	}{
		{
			name: "matches",
			object: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "foo",
				},
			},
			expect: true,
		},
		{
			name: "does not match ",
			object: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "bar",
				},
			},
			expect: false,
		},

		{
			name: "invalid",
			object: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{},
			},
			expect: false,
		},
	}

	for _, tc := range tests {
		require.Equal(t, tc.expect, matchesControllerName("foo")(tc.object))
	}
}
