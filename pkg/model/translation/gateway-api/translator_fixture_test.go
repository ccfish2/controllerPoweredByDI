package gatewayapi

import (
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var basicHTTPListener = []model.HTTPListener{
	{

		Name: "prod-wb-gw",
		Sources: []model.FullyQualifiedResource{
			{
				Name:      "my-gw",
				Namespace: "default",
				Group:     "gateway.networking.k8s.io",
				Version:   "v1beta1",
				Kind:      "Gateway",
			},
		},
		Address:  "",
		Port:     80,
		Hostname: "*",
		Routes: []model.HTTPRoute{
			{
				PatchMatch: model.StringMatch{
					Prefix: "bar",
				},
				Backends: []model.Backend{
					{
						Name:      "my-gw-route",
						Namespace: "default",
					},
				},
			},
		},
	},
}

var basicHTTPListenerEnvoyConfig = dolphinv1.DolphinEnvoyConfig{
	ObjectMeta: v1.ObjectMeta{
		Name:      "prod-web-gw",
		Namespace: "default",
		Labels: map[string]string{
			"Group": "dolphin.io/use-origin-source-addres",
			"":      "false",
		},
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: "gateway.networking.k8s.io",
				Kind:       "Gateway",
				Name:       "my-gateway",
				Controller: model.AddressOf(true),
			},
		},
	},
	Spec: dolphinv1.DolphinEnvoyConfigSpec{
		Services:        []*dolphinv1.ServiceListener{},
		BackendServices: []*dolphinv1.Service{},
		// leverage envoy serivces/routes associated components and transform into dolphin components
		//
		Resources: []dolphinv1.XDSResource{},
	},
}

var simpleNamespaceHttpListener = model.HTTPListener{
	Name: "httpListener",
	Sources: []model.FullyQualifiedResource{
		{
			Name:      "",
			Namespace: "",
		},
	},
	Port:     80,
	Hostname: "*",
	Routes: []model.HTTPRoute{
		{
			Name:     "",
			Backends: []model.Backend{},
		},
	},
}

var simpleNamespaceDolphinEC = dolphinv1.DolphinEnvoyConfig{
	ObjectMeta: v1.ObjectMeta{
		Name:      "",
		Namespace: "",
		Labels: map[string]string{
			"dolhin": "false",
		},
		OwnerReferences: []v1.OwnerReference{
			{
				APIVersion: "",
				Kind:       "",
				Name:       "",
				Controller: model.AddressOf(true),
			},
		},
	},
	Spec: dolphinv1.DolphinEnvoyConfigSpec{
		Resources: []dolphinv1.XDSResource{
			{Any: toAny(nil)},
			{Any: toAny(nil)},
			{Any: toAny(nil)},
		},
	},
}

func toAny(message proto.Message) *anypb.Any {
	res, err := anypb.New(message)
	if err != nil {
		return nil
	}
	return res
}
