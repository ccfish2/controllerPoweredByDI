package ingress

import (
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"
	"github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

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
	panic("unimpl")
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
	panic("unimpl")
}

func (r *ingressReconciler) enqueSharedDolphinIngress() handler.EventHandler {
	panic("unimpl")
}

func (r *ingressReconciler) enqIngressWithExplicitControll() handler.EventHandler {
	panic("unimpl")
}

func (r *ingressReconciler) forSharedLBService() builder.WatchesOption {
	panic("unimpl")
}

func (r *ingressReconciler) forShaedDolphinEnvoyConfig() builder.WatchesOption {
	panic("unimpl")
}

func (r *ingressReconciler) enqPsedoIngress() handler.EventHandler {
	panic("unimpl")
}

func (r *ingressReconciler) forDolphinIngressClass() builder.WatchesOption {
	panic("unimpl")
}