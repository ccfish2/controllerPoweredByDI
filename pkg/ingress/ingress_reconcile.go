package ingress

import (
	"context"
	"strings"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ingressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// scopedlog the controller is ingress
	// read ingress objects from kubeapi server client with cached which namespace and name is the same as from req

	// check metaobject creationtimestamp fields, if it is set, this is foreground deletion, skip this
	// until finalizers are empty

	// if the ingress is not managed by dolphin anymore, remove and clean them
	// update routing within the load balancer
	return ctrl.Result{}, nil
}

func (r *ingressReconciler) createOrUpdateDedicatedResource(ctx context.Context, ingress *networkingv1.Ingress) error {

	return nil
}

// generic way, but specifically for shared load balance
func (r *ingressReconciler) propagateAnnotationsAndLabels(ingress *networkingv1.Ingress, objectMeta *metav1.ObjectMeta) {

}

func (r *ingressReconciler) createSharedResources(ctx context.Context) error {
	return nil
}

func (r *ingressReconciler) cleanupDedicatedResources(ctx context.Context, lim types.NamespacedName) error {
	return nil
}

func (r *ingressReconciler) cleanupSharedResources(ctx context.Context, lim types.NamespacedName) error {
	return nil
}

func (r *ingressReconciler) buildDedicatedResources(ctx context.Context) (*dolphinv1.DolphinEnvoyConfig, error) {
	panic("impl")
}

func (r *ingressReconciler) buildSharedResources(ctx context.Context) (*dolphinv1.DolphinEnvoyConfig, *corev1.Service, *corev1.Endpoints, error) {
	panic("impl")
}

func (r *ingressReconciler) createOrUpdateEnvoyConfig(ctx context.Context, dec *dolphinv1.DolphinEnvoyConfig) error {
	panic("impl")
}

func (r *ingressReconciler) createOrUpdateService(ctx context.Context, svc *corev1.Service) error {
	panic("impl")
}

func (r *ingressReconciler) createOrUpdateEndpoints(ctx context.Context, dec *corev1.Endpoints) error {
	panic("impl")
}

func mergeMap(src, dst map[string]string, prefixes ...string) map[string]string {
	if src == nil || len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}

	for key, value := range src {
		if atLeastOnePrefixMatch(key, prefixes) {
			dst[key] = value
		}
	}
	return dst
}

func atLeastOnePrefixMatch(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
