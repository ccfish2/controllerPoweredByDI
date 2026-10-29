package gateway_api

import (
	"context"
	"log/slog"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"

	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/indexers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

func (h *SecretSyncHandler) EnqueueTLSSecrets(c client.Client, logger logrus.FieldLogger) handler.EventHandler {
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

func (h *SecretSyncHandler) IsReferencedByDolphinGateway(ctx context.Context, c client.Client, logger logrus.FieldLogger, obj *corev1.Secret) bool {
	gateway := getGatewaysForSecret(ctx, c, obj, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	for _, gw := range gateway {
		if hasMatchingController(ctx, c, controllerName, slog.New(slog.NewTextHandler(os.Stdout, nil)))(gw) {
			return true
		}
	}
	return false
}

type SecretSyncHandler struct {
	client         client.Client
	logger         *slog.Logger
	controllerName string
}

func NewSecretSyncHandler(c client.Client, logger *slog.Logger, controllerName string) *SecretSyncHandler {
	return &SecretSyncHandler{
		client:         c,
		logger:         logger,
		controllerName: controllerName,
	}
}

func (h *SecretSyncHandler) EnqueueListenerSetTLSSecrets() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		scopedLog := h.logger.With(
			logfields.Resource, obj.GetName(),
		)

		ls, ok := obj.(*gatewayv1.ListenerSet)
		if !ok {
			return nil
		}

		gw := &gatewayv1.Gateway{}
		gwNN := helpers.ListenerSetParentGateway(ls)
		if err := h.client.Get(ctx, *gwNN, gw); err != nil {
			scopedLog.DebugContext(ctx, "Unable to get parent gateway for listenerset", logfields.Error, err)
			return nil
		}

		// Check whether parent Gateway is managed by dolphin
		if !helpers.GatewayHasMatchingControllerFn(ctx, h.client, h.controllerName, h.logger)(gw) {
			return nil
		}

		var reqs []reconcile.Request
		for _, l := range ls.Spec.Listeners {
			if l.TLS == nil {
				continue
			}
			for _, cert := range l.TLS.CertificateRefs {
				if !helpers.IsSecret(cert) {
					continue
				}
				s := types.NamespacedName{
					Namespace: helpers.NamespaceDerefOr(cert.Namespace, ls.Namespace),
					Name:      string(cert.Name),
				}
				reqs = append(reqs, reconcile.Request{NamespacedName: s})
				scopedLog.DebugContext(ctx, "Enqueued secret for listenerset", logfields.Secret, s)
			}
		}
		return reqs
	})
}

// Enqueue BackendTLSPolicyConfigmaps produces a handler.EventHandler that, when it is passed a
// BackendTLSPolicy as the object.Object, returns any ConfigMaps referenced.
func (h *SecretSyncHandler) EnqueueBackendTLSPolicyConfigMaps() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		scopedLog := h.logger.With(
			logfields.Resource, obj.GetName(),
		)

		var reqs []reconcile.Request

		btlsp, ok := obj.(*gatewayv1.BackendTLSPolicy)
		if !ok {
			return nil
		}

		for _, certRef := range btlsp.Spec.Validation.CACertificateRefs {
			if IsConfigMap(certRef) {
				continue
			}
			cfg := types.NamespacedName{
				Namespace: btlsp.GetNamespace(),
				Name:      string(certRef.Name),
			}
			reqs = append(reqs, reconcile.Request{NamespacedName: cfg})
			scopedLog.DebugContext(ctx, "Enqueued configmap for backendtlspolicy", "configMapName", cfg)
		}
		return reqs
	})
}

func (h *SecretSyncHandler) ConfigMapIsReferencedInGateway(ctx context.Context, _ client.Client, _ *slog.Logger, cfgMap *corev1.ConfigMap) bool {
	scopedLog := h.logger.With(logfields.LogSubsys, "queue-gw-from-backendtlspolicy-configmap")

	cfgMapName := client.ObjectKeyFromObject(cfgMap)

	btlspList := &gatewayv1.BackendTLSPolicyList{}

	if err := h.client.List(ctx, btlspList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(indexers.BackendTLSPolicyConfigMapIndex, cfgMapName.String()),
	}); err != nil {
		scopedLog.ErrorContext(ctx, "Failed to get related BackendTLSPolicies for ConfigMap", logfields.Error, err)
		return false
	}
	if len(btlspList.Items) == 0 {
		return false
	}

	for _, btlsp := range btlspList.Items {
		for _, ancestorStatus := range btlsp.Status.Ancestors {
			if string(ancestorStatus.ControllerName) == h.controllerName && helpers.IsAccepted(ancestorStatus.Conditions) {
				return true
			}
		}
	}
	return false
}
