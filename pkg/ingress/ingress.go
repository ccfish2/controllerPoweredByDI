package ingress

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/pkg/ingress/annotations"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/translation"
	ingressTranslation "github.com/ccfish2/controllerPoweredByDI/pkg/model/translation/ingress"
	"github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

const (
	dolphinIngressPrefix    = "dolphin-ingress"
	dolphinIngressClassName = "dolphin"
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
		logger: logger,
		client: c,

		sharedTranslator:    ingressTranslation.NewSharedIngressTranslator(sharedLBServiceName, dolphinNamespace, secretsNamespace, enforceHTTPS, useProxyProtocol, proxyIdleTimeoutSeconds),
		dedicatedTranslator: ingressTranslation.NewDedicatedIngressTranslator(secretsNamespace, enforceHTTPS, useProxyProtocol, proxyIdleTimeoutSeconds),

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
		Watches(&networkingv1.IngressClass{}, r.enqueueIngressesWithoutExplicitClass(), r.forDolphinIngressClass(), withDefaultIngressClassAnnotation()).
		Complete(r)
}

func (r *ingressReconciler) isEffectiveLoadbalancerModeDedicated(ingress *networkingv1.Ingress) bool {
	value := annotations.GetAnnotationIngressLoadbalancerMode(ingress)
	switch value {
	case annotations.LoadbalancerModeDedicated:
		return true
	case annotations.LoadbalancerModeShared:
		return false
	default:
		return r.defaultLoadbalancerMode == annotations.LoadbalancerModeDedicated
	}
}

func (r *ingressReconciler) enqueSharedDolphinIngress() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		// use the client list ingress
		var ingresslist networkingv1.IngressList
		if err := r.client.List(ctx, &ingresslist, &client.ListOptions{}); err != nil {
			return []reconcile.Request{}
		}

		res := []reconcile.Request{}
		for _, in := range ingresslist.Items {
			if !isdolphinManagedIngress(ctx, r.client, r.logger, in) {
				continue
			}
			if !r.isEffectiveLoadbalancerModeDedicated(&in) {
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

func (r *ingressReconciler) enqueueIngressesWithoutExplicitClass() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		inglist := &networkingv1.IngressList{}
		if err := r.client.List(ctx, inglist, &client.ListOptions{}); err != nil {
			return nil
		}

		res := []reconcile.Request{}
		for _, in := range inglist.Items {
			if in.Spec.IngressClassName != nil {
				continue
			}
			res = append(res, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: in.Namespace,
					Name:      in.Name,
				}})
		}
		return res
	})
}

func (r *ingressReconciler) forSharedLBService() builder.WatchesOption {
	return builder.WithPredicates(&matchesInstancePredicate{namespace: r.dolphinNamespace, name: r.sharedLBServiceName})
}

func (r *ingressReconciler) forShaedDolphinEnvoyConfig() builder.WatchesOption {
	return builder.WithPredicates(&matchesInstancePredicate{namespace: r.dolphinNamespace, name: r.sharedLBServiceName})
}

func (r *ingressReconciler) forDolphinIngressClass() builder.WatchesOption {
	return builder.WithPredicates(&matchesInstancePredicate{namespace: "", name: dolphinIngressClassName})
}

func withDefaultIngressClassAnnotation() builder.WatchesOption {
	return builder.WithPredicates(&defaultIngressClassPredicate{})
}

func (r *ingressReconciler) forDlphinManagedController() builder.ForOption {
	return builder.WithPredicates(&matchesDolphinRelevantIngressPredicate{client: r.client, logger: r.logger})
}

var _ predicate.Predicate = &matchesInstancePredicate{}

type matchesInstancePredicate struct {
	namespace string
	name      string
}

// Create implements predicate.TypedPredicate.
func (r *matchesInstancePredicate) Create(event event.CreateEvent) bool {
	return event.Object.GetNamespace() == r.namespace && event.Object.GetName() == r.name
}

func (r *matchesInstancePredicate) Update(event event.UpdateEvent) bool {
	return event.ObjectNew.GetNamespace() == r.namespace && event.ObjectNew.GetName() == r.name
}

func (r *matchesInstancePredicate) Delete(event event.DeleteEvent) bool {
	return event.Object.GetNamespace() == r.namespace && event.Object.GetName() == r.name
}

func (r *matchesInstancePredicate) Generic(event event.GenericEvent) bool {
	return event.Object.GetNamespace() == r.namespace && event.Object.GetName() == r.name
}

var _ predicate.Predicate = &defaultIngressClassPredicate{}

type defaultIngressClassPredicate struct {
	namespace string
	name      string
}

// Create implements predicate.TypedPredicate.
func (r *defaultIngressClassPredicate) Create(event event.CreateEvent) bool {
	return r.isIngressClassMarkedAsDefault(event.Object)
}

func (r *defaultIngressClassPredicate) Update(event event.UpdateEvent) bool {
	oldIngressClassDefault := r.isIngressClassMarkedAsDefault(event.ObjectOld)
	newIngressClassDefault := r.isIngressClassMarkedAsDefault(event.ObjectNew)

	return (oldIngressClassDefault || newIngressClassDefault) &&
		// Only in case of a change
		oldIngressClassDefault != newIngressClassDefault
}

func (r *defaultIngressClassPredicate) Delete(event event.DeleteEvent) bool {
	return r.isIngressClassMarkedAsDefault(event.Object)
}

func (r *defaultIngressClassPredicate) Generic(event event.GenericEvent) bool {
	return r.isIngressClassMarkedAsDefault(event.Object)
}

func (r *defaultIngressClassPredicate) isIngressClassMarkedAsDefault(o client.Object) bool {
	ingressClass, ok := o.(*networkingv1.IngressClass)
	if !ok {
		return false
	}

	isDefault, err := isIngressClassMarkedAsDefault(*ingressClass)
	if err != nil {
		return false
	}

	return isDefault
}

func (r *ingressReconciler) enqPsedoIngress() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: r.dolphinNamespace,
					Name:      "pseudo-ingress",
				},
			},
		}
	})
}

var _ predicate.Predicate = &matchesDolphinRelevantIngressPredicate{}

type matchesDolphinRelevantIngressPredicate struct {
	client client.Client
	logger logrus.FieldLogger
}

func (r *matchesDolphinRelevantIngressPredicate) Create(event event.CreateEvent) bool {
	return r.isCiliumManagedIngress(event.Object)
}

func (r *matchesDolphinRelevantIngressPredicate) Update(event event.UpdateEvent) bool {
	return r.isCiliumManagedIngress(event.ObjectOld) || r.isCiliumManagedIngress(event.ObjectNew)
}

func (r *matchesDolphinRelevantIngressPredicate) Delete(event event.DeleteEvent) bool {
	return r.isCiliumManagedIngress(event.Object)
}

func (r *matchesDolphinRelevantIngressPredicate) Generic(event event.GenericEvent) bool {
	return r.isCiliumManagedIngress(event.Object)
}

func (r *matchesDolphinRelevantIngressPredicate) isCiliumManagedIngress(o client.Object) bool {
	ingress, ok := o.(*networkingv1.Ingress)
	if !ok {
		return false
	}

	return isdolphinManagedIngress(context.Background(), r.client, r.logger, *ingress)
}
