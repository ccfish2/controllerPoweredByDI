package ingress

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"
	"github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

type ingressReconciler struct {
	logger logrus.FieldLogger
	client client.Client

	maxRetries              int
	enforcedHTTPS           bool
	useProxyProtocol        bool
	secretsNamespace        string
	lbAnnotationPrefixes    []string
	sharedLBServiceName     string
	dolphinNamespace        string
	defaultLoadbalancerMode string
	defaultSecretNamespace  string
	defaultSecretName       string
	idleTimeoutSeconds      int

	sharedTranslator    translation.Translator
	dedicatedTranslator translation.Translator
}

func newIngressReconciler(
	logger logrus.FieldLogger,
	c client.Client,
	dolphinNamespace string,
	enforceHTTPS bool,
	useProxyProtocol bool,
	secretsNamespace string,
	lbAnnotationPrefixes []string,
	sharedLBServiceName string,
	defaultLoadbalancerMode string,
	defaultSecretNamespace string,
	defaultSecretName string,
	proxyIdleTimeoutSeconds int,
) *ingressReconciler {
	return &ingressReconciler{
		logger:                  logger,
		client:                  c,
		maxRetries:              3,
		enforcedHTTPS:           enforceHTTPS,
		useProxyProtocol:        useProxyProtocol,
		secretsNamespace:        secretsNamespace,
		lbAnnotationPrefixes:    lbAnnotationPrefixes,
		sharedLBServiceName:     sharedLBServiceName,
		defaultLoadbalancerMode: defaultLoadbalancerMode,
		defaultSecretNamespace:  defaultSecretNamespace,
		defaultSecretName:       defaultSecretName,
		idleTimeoutSeconds:      proxyIdleTimeoutSeconds,
		dolphinNamespace:        "dolphin",
	}
}

func (r *ingressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}, r.forDlphinManagedController()).
		Owns(&corev1.Service{}).
		Owns(&corev1.Endpoints{}).
		Owns(&dolphinv1.DolphinEnvoyConfig{}).
		Watches(&corev1.Service{}, r.enqueSharedDolphinIngress(), r.forSharedLBService()).
		Watches(&dolphinv1.DolphinEnvoyConfig{}, r.enqPsedoIngress(), r.forShaedDolphinEnvoyConfig()).
		Watches(&networkingv1.IngressClass{}, r.enqIngressWithExplicitControll(), r.forDolphinIngressClass()).
		Complete(r)
}

func (r *ingressReconciler) forDlphinManagedController() builder.ForOption {
	return builder.WithPredicates()
}

func isDolphinmanagedIngress(ctx context.Context, c client.Client, log logrus.FieldLogger, ing *networkingv1.Ingress) bool {
	return true
}

func isEffectiveLoadbalancerModeDedicated(in *networkingv1.Ingress) bool {
	return true
}

func (r *ingressReconciler) enqueSharedDolphinIngress() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		//panic("unimpl")
		// use the client list ingress
		var ingresslist networkingv1.IngressList
		if err := r.client.List(ctx, &ingresslist, &client.ListOptions{}); err != nil {
			return []reconcile.Request{}
		}

		res := []reconcile.Request{}
		for _, in := range ingresslist.Items {
			if !isDolphinmanagedIngress(ctx, r.client, r.logger, &in) {
				continue
			}
			if !isEffectiveLoadbalancerModeDedicated(&in) {
				continue
			}
			res = append(res, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: in.Namespace,
					Name:      in.Name,
				},
			})
		}
		return res
	})
}

func (r *ingressReconciler) enqIngressWithExplicitControll() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		panic("unimpl")
	})
}

func (r *ingressReconciler) forSharedLBService() builder.WatchesOption {
	return builder.WithPredicates()
}

func (r *ingressReconciler) forShaedDolphinEnvoyConfig() builder.WatchesOption {
	return builder.WithPredicates()
}

func (r *ingressReconciler) enqPsedoIngress() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		panic("unimpl")
	})
}

func (r *ingressReconciler) forDolphinIngressClass() builder.WatchesOption {
	return builder.WithPredicates()
}
