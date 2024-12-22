package ingress

import (
	"context"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func EnqueueReferencedTLSSecrets(c client.Client, logger logrus.FieldLogger) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		// read ns, name, tls info from obj
		// and compose the request
		// enqueue thos request
		return []reconcile.Request{}
	})
}

func IsReferencedByDolphinIngress(ctx context.Context, c client.Client, logger logrus.FieldLogger, obj *corev1.Secret) bool {
	// get ingress using the obj namespace
	return true
}

func enqueueAllSecrets(c client.Client) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return []reconcile.Request{}
	})
}
