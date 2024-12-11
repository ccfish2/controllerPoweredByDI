package ingestion

import (
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var basiccHTTP = Input{
	GatewayClass: gatewayv1.GatewayClass{},
	Gateway: gatewayv1.Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-gateway",
			Namespace: "",
		},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{
					Name: "prod-gw-web",

					Port:     gatewayv1.PortNumber(80),
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
			Infrastructure: &gatewayv1.GatewayInfrastructure{Labels: map[gatewayv1.LabelKey]gatewayv1.LabelValue{},
				Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{}},
		},
	},
	HTTPRoutes: []gatewayv1.HTTPRoute{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      "http-app-1",
				Namespace: "",
			},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{Name: "my-gw"},
					},
				},
				Rules: []gatewayv1.HTTPRouteRule{
					{
						Matches: []gatewayv1.HTTPRouteMatch{
							{
								Path: &gatewayv1.HTTPPathMatch{
									Type:  model.AddressOf[gatewayv1.PathMatchType]("PrefixMatch"),
									Value: model.AddressOf("/var"),
								},
							},
						},
					},
				},
			},
		},
	},
	Services: []corev1.Service{
		{
			ObjectMeta: v1.ObjectMeta{
				Name:      "my-servce",
				Namespace: "same",
			},
		},
	},
}
