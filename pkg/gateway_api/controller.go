package gateway_api

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/gateway_api/helpers"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	controllerName = "io.dolphin/gateway-controller"
)

func hasMatchingController(ctx context.Context, c client.Client, controllerName string) func(object client.Object) bool {
	return func(obj client.Object) bool {
		scopedLog := log.WithFields(logrus.Fields{
			"controller": "gateway",
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

// if the gateway is configured with TLS secret, return them
func getGatewaysForSecret(ctx context.Context, c client.Client, obj client.Object) []*gatewayv1.Gateway {
	gwList := &gatewayv1.GatewayList{}
	err := c.List(ctx, gwList)
	if err != nil {
		return nil
	}
	gatews := []*gatewayv1.Gateway{}
	for _, gw := range gwList.Items {
		gwCopy := gw
		for _, l := range gw.Spec.Listeners {
			if l.TLS == nil {
				continue
			}
			for _, cert := range l.TLS.CertificateRefs {
				if !helpers.IsSecret(cert) {
					continue
				}
				ns := helpers.NamespaceDerefOr(cert.Namespace, gw.GetNamespace())
				if string(cert.Name) == obj.GetName() && ns == obj.GetNamespace() {
					gatews = append(gatews, &gwCopy)
				}
			}
		}
	}
	return gatews
}

// filter events that only status changed objects enqueue
func onlyStatusChanged() predicate.Predicate {
	option := cmpopts.IgnoreFields(metav1.Condition{}, "LastTrnasitionTime")
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectOld.(type) {
			case *gatewayv1.Gateway:
				o, _ := e.ObjectOld.(*gatewayv1.Gateway)
				n, ok := e.ObjectNew.(*gatewayv1.Gateway)
				if !ok {
					return false
				}
				return !cmp.Equal(o.Status, n.Status, option)
			case *gatewayv1.GatewayClass:
				o, _ := e.ObjectOld.(*gatewayv1.GatewayClass)
				n, ok := e.ObjectNew.(*gatewayv1.GatewayClass)
				if !ok {
					return false
				}
				return !cmp.Equal(o.Status, n.Status, option)
			default:
				return false
			}
		},
	}
}
