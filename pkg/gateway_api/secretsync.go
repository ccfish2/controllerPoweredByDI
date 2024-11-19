package gateway_api

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/pkg/gateway_api/helpers"
)

func EnqueueTLSSecrets(c client.Client, logger logrus.FieldLogger) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		scopedLog := logger.WithFields(logrus.Fields{
			"controller": "secrets",
			"resource":   obj.GetName(),
		})

		gw, ok := obj.(*gatewayapi.Gateway)
		if !ok {
			return nil
		}

		var reqs []reconcile.Request
		for _, l := range gw.Spec.Listeners {
			if l.TLS == nil {
				continue
			}
			for _, cert := range l.TLS.CertificateRefs {
				if !helpers.IsSecret(cert) {
					continue
				}

				s := types.NamespacedName{
					Namespace: helpers.NamespaceDerefOr(cert.Namespace, gw.Namespace),
					Name:      string(cert.Name),
				}
				reqs = append(reqs, reconcile.Request{NamespacedName: s})
				scopedLog.WithField("secret", s).Debug(
					"enqueued secret for gteway",
				)
			}
		}
		return reqs
	})
}

func IsReferencedByDolphinGateway(ctx context.Context, c client.Client, logger logrus.FieldLogger, obj *corev1.Secret) bool {
	gateway := getGatewaysForSecret(ctx, c, obj)
	for _, gw := range gateway {
		if hasMatchingController(ctx, c, controllerName)(gw) {
			return true
		}
	}
	return false
}
