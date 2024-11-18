package gatewayapi

import (
	"context"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	controllerName = "io.dolphin/gateway-controller"
)

func hasMatchingController(ctx context.Context, c client.Client, controllerName string) func(object client.Object) bool {
	return func(obj client.Object) bool {
		scopedLog := log.WithFields(logrus.Fields{
			"controller": gateway,
			"resource":   obj.GetName(),
		})
		gw, ok := obj.(*gatewayv1.Gateway)
		if !ok {
			return false
		}
		gwc := &gatewayv1.GatewayClass{}
		key := types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}
		if err := c.Get(ctx, key, gwc); err != nil {
			scopedLog.WithError(err).Error("unable to get gatewayClass")
		}
		return string(gwc.Spec.ControllerName) == controllerName
	}
}

func getGatewaysForSecret(ctx context.Context, c client.Client, obj client.Object) []*gatewayv1.Gateway {
	panic("")
}
