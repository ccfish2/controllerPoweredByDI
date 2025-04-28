package ingress

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/ccfish2/controllerPoweredByDI/pkg/ingress/annotations"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model"
	"github.com/ccfish2/controllerPoweredByDI/pkg/model/ingestion"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

func (r *ingressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := r.logger.WithFields(logrus.Fields{
		logfields.Controller: "ingress",
		logfields.Resource:   req.NamespacedName,
	})

	scopedLog.Info("Reconciling Ingress")
	ingress := &networkingv1.Ingress{}
	if err := r.client.Get(ctx, req.NamespacedName, ingress); err != nil {
		if !k8serrors.IsNotFound(err) {
			return controllerruntime.Fail(fmt.Errorf("failed to get Ingress: %w", err))
		}
		// try to cleanup shared dolphinEnvoyConfig
		scopedLog.Debug("Trying to cleanup potentially existing resources of deleted Ingress")
		if err := r.tryCleanupSharedResources(ctx); err != nil {
			return controllerruntime.Fail(err)
		}

		return controllerruntime.Success()
	}

	// skip when (DeletionTimestamp set)
	if ingress.GetDeletionTimestamp() != nil {
		scopedLog.Debug("Ingress is marked for deletion - waiting for actual deletion")
		return controllerruntime.Success()
	}

	// Ingress is no longer managed by dolphin.
	// Trying to cleanup resources.
	if !isdolphinManagedIngress(ctx, r.client, r.logger, *ingress) {
		scopedLog.Debug("Trying to cleanup potentially existing resources of unmanaged Ingress")
		if err := r.tryCleanupSharedResources(ctx); err != nil {
			return controllerruntime.Fail(err)
		}

		if err := r.tryCleanupDedicatedResources(ctx, req.NamespacedName); err != nil {
			return controllerruntime.Fail(err)
		}

		scopedLog.Debug("Trying to cleanup Ingress status of unmanaged Ingress")
		if err := r.tryCleanupIngressStatus(ctx, ingress); err != nil {
			scopedLog.WithError(err).Warn("Failed to cleanup Ingress status")
		}

		scopedLog.Info("Successfully cleaned Ingress resources")
		return controllerruntime.Success()
	}

	// Creation / Update of Ingress resources depending on the loadbalancer mode
	if r.isEffectiveLoadbalancerModeDedicated(ingress) {
		scopedLog.Debug("Updating dedicated resources")
		if err := r.createOrUpdateDedicatedResources(ctx, ingress); err != nil {
			if k8serrors.IsForbidden(err) && k8serrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				scopedLog.Info("Aborting reconciliation because namespace is being terminated")
				return controllerruntime.Success()
			}

			return controllerruntime.Fail(err)
		}

		// Trying to cleanup shared resources (potential change of LB mode)
		scopedLog.Debug("Trying to cleanup potentially existing shared resources")
		if err := r.tryCleanupSharedResources(ctx); err != nil {
			return controllerruntime.Fail(err)
		}
	} else {
		scopedLog.Debug("Updating shared resources")
		if err := r.createOrUpdateSharedResources(ctx); err != nil {
			return controllerruntime.Fail(err)
		}

		// Trying to cleanup dedicated resources (potential change of LB mode)
		scopedLog.Debug("Trying to cleanup potentially existing dedicated resources")
		if err := r.tryCleanupDedicatedResources(ctx, req.NamespacedName); err != nil {
			return controllerruntime.Fail(err)
		}
	}

	// Update status
	scopedLog.Debug("Updating Ingress status")
	if err := r.updateIngressLoadbalancerStatus(ctx, ingress); err != nil {
		return controllerruntime.Fail(fmt.Errorf("failed to update Ingress loadbalancer status: %w", err))
	}

	scopedLog.Info("Successfully reconciled Ingress")
	return controllerruntime.Success()
}

func (r *ingressReconciler) tryCleanupIngressStatus(ctx context.Context, ingress *networkingv1.Ingress) error {
	ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{}

	if err := r.client.Status().Update(ctx, ingress); err != nil {
		return fmt.Errorf("failed to update Ingress status: %w", err)
	}

	return nil
}

func (r *ingressReconciler) createOrUpdateDedicatedResources(ctx context.Context, ingress *networkingv1.Ingress) error {
	desiredDolphinEnvoyConfig, desiredService, desiredEndpoints, err := r.buildDedicatedResources(ctx, ingress)
	if err != nil {
		return fmt.Errorf("failed to build dedicated resources: %w", err)
	}

	if err := r.createOrUpdateDolphinEnvoyConfig(ctx, desiredDolphinEnvoyConfig); err != nil {
		return err
	}

	if err := r.createOrUpdateService(ctx, desiredService); err != nil {
		return err
	}

	if err := r.createOrUpdateEndpoints(ctx, desiredEndpoints); err != nil {
		return err
	}

	return nil
}

func (r *ingressReconciler) createOrUpdateDolphinEnvoyConfig(ctx context.Context, desiredCEC *dolphinv1.DolphinEnvoyConfig) error {
	dec := desiredCEC.DeepCopy()

	result, err := controllerutil.CreateOrUpdate(ctx, r.client, dec, func() error {
		dec.Spec = desiredCEC.Spec
		dec.OwnerReferences = desiredCEC.OwnerReferences
		dec.Annotations = mergeMap(dec.Annotations, desiredCEC.Annotations)
		dec.Labels = mergeMap(dec.Labels, desiredCEC.Labels)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update DolphinEnvoyConfig: %w", err)
	}
	r.logger.Debugf("DolphinEnvoyConfig %s has been %s", client.ObjectKeyFromObject(dec), result)

	return nil
}

// this is the L7 routes part, ingress
func (r *ingressReconciler) buildDedicatedResources(ctx context.Context, ingress *networkingv1.Ingress) (*dolphinv1.DolphinEnvoyConfig, *corev1.Service, *corev1.Endpoints, error) {
	m := &model.Model{}

	if annotations.GetAnnotationTLSPassthroughEnabled(ingress) {
		m.TLS = append(m.TLS, ingestion.IngressPassthrough(*ingress, r.defaultSecretNamespace, r.defaultSecretName)...)
	} else {
		m.HTTP = append(m.HTTP, ingestion.Ingress(*ingress, r.defaultSecretNamespace, r.defaultSecretName)...)
	}

	dec, svc, ep, err := r.dedicatedTranslator.Translate(m)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to translate model into resources: %w", err)
	}

	r.propagateIngressAnnotationsAndLabels(ingress, &svc.ObjectMeta)

	// Explicitly set the controlling OwnerReference on the DolphinEnvoyConfig
	if err := controllerutil.SetControllerReference(ingress, dec, r.client.Scheme()); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to set controller reference on DolphinEnvoyConfig: %w", err)
	}

	return dec, svc, ep, err
}

func (r *ingressReconciler) updateIngressLoadbalancerStatus(ctx context.Context, ingress *networkingv1.Ingress) error {
	serviceNamespacedName := types.NamespacedName{}
	if r.isEffectiveLoadbalancerModeDedicated(ingress) {
		serviceNamespacedName.Namespace = ingress.Namespace
		serviceNamespacedName.Name = fmt.Sprintf("%s-%s", dolphinIngressPrefix, ingress.Name)
	} else {
		serviceNamespacedName.Namespace = r.dolphinNamespace
		serviceNamespacedName.Name = r.sharedLBServiceName
	}

	loadbalancerService := corev1.Service{}
	if err := r.client.Get(ctx, serviceNamespacedName, &loadbalancerService); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get loadbalancer Service: %w", err)
		}
		return nil
	}

	ingress.Status.LoadBalancer.Ingress = convertToNetworkV1IngressLoadBalancerIngress(loadbalancerService.Status.LoadBalancer.Ingress)

	if err := r.client.Status().Update(ctx, ingress); err != nil {
		return fmt.Errorf("failed to write Ingress status: %w", err)
	}

	return nil
}

func convertToNetworkV1IngressLoadBalancerIngress(lbIngresses []corev1.LoadBalancerIngress) []networkingv1.IngressLoadBalancerIngress {
	if lbIngresses == nil {
		return nil
	}

	ingLBIngs := make([]networkingv1.IngressLoadBalancerIngress, 0, len(lbIngresses))
	for _, lbIng := range lbIngresses {
		ports := make([]networkingv1.IngressPortStatus, 0, len(lbIng.Ports))
		for _, port := range lbIng.Ports {
			ports = append(ports, networkingv1.IngressPortStatus{
				Port:     port.Port,
				Protocol: corev1.Protocol(port.Protocol),
				Error:    port.Error,
			})
		}
		ingLBIngs = append(ingLBIngs,
			networkingv1.IngressLoadBalancerIngress{
				IP:       lbIng.IP,
				Hostname: lbIng.Hostname,
				Ports:    ports,
			})
	}

	return ingLBIngs
}

// generic way, but specifically for shared load balance
func (r *ingressReconciler) propagateIngressAnnotationsAndLabels(ingress *networkingv1.Ingress, objectMeta *metav1.ObjectMeta) {
	if len(r.lbAnnotationPrefixes) > 0 {
		objectMeta.Annotations = mergeMap(objectMeta.Annotations, ingress.Annotations, r.lbAnnotationPrefixes...)
		objectMeta.Labels = mergeMap(objectMeta.Labels, ingress.Labels, r.lbAnnotationPrefixes...)
	}
}

func (r *ingressReconciler) createOrUpdateSharedResources(ctx context.Context) error {
	desiredDolphinEnvoyConfig, err := r.buildSharedResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to build shared resources: %w", err)
	}

	if err := r.createOrUpdateDolphinEnvoyConfig(ctx, desiredDolphinEnvoyConfig); err != nil {
		return err
	}

	return nil
}

func (r *ingressReconciler) tryCleanupDedicatedResources(ctx context.Context, ingressNamespacedName types.NamespacedName) error {
	resources := map[client.Object]types.NamespacedName{
		&corev1.Service{}:               {Namespace: ingressNamespacedName.Namespace, Name: fmt.Sprintf("%s-%s", dolphinIngressPrefix, ingressNamespacedName.Name)},
		&corev1.Endpoints{}:             {Namespace: ingressNamespacedName.Namespace, Name: fmt.Sprintf("%s-%s", dolphinIngressPrefix, ingressNamespacedName.Name)},
		&dolphinv1.DolphinEnvoyConfig{}: {Namespace: ingressNamespacedName.Namespace, Name: fmt.Sprintf("%s-%s-%s", dolphinIngressPrefix, ingressNamespacedName.Namespace, ingressNamespacedName.Name)},
	}

	for k, v := range resources {
		if err := r.tryDeletingResource(ctx, k, v); err != nil {
			return err
		}
	}

	return nil
}

func (r *ingressReconciler) tryCleanupSharedResources(ctx context.Context) error {
	desiredDolphinenvoyConfig, err := r.buildSharedResources(ctx)
	if err != nil {
		return fmt.Errorf("failed to build shared resources: %w", err)
	}
	if err := r.createOrUpdateDolphinEnvoyConfig(ctx, desiredDolphinenvoyConfig); err != nil {
		return err
	}
	return nil
}

func (r *ingressReconciler) tryDeletingResource(ctx context.Context, object client.Object, namespacedName types.NamespacedName) error {
	if err := r.client.Get(ctx, namespacedName, object); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get existing %T: %w", object, err)
		}
		return nil
	}

	if err := r.client.Delete(ctx, object); err != nil {
		return fmt.Errorf("failed to delete existing %T: %w", object, err)
	}

	return nil
}

func (r *ingressReconciler) buildSharedResources(ctx context.Context) (*dolphinv1.DolphinEnvoyConfig, error) {
	ingressList := networkingv1.IngressList{}
	if err := r.client.List(ctx, &ingressList); err != nil {
		return nil, fmt.Errorf("failed to list Ingresses: %w", err)
	}

	m := &model.Model{}
	allSharedIngresses := ingressList.Items
	slices.SortStableFunc(allSharedIngresses, func(a, b networkingv1.Ingress) int {
		return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	for _, item := range allSharedIngresses {
		if !isdolphinManagedIngress(ctx, r.client, r.logger, item) || r.isEffectiveLoadbalancerModeDedicated(&item) || item.GetDeletionTimestamp() != nil {
			continue
		}
		if annotations.GetAnnotationTLSPassthroughEnabled(&item) {
			m.TLS = append(m.TLS, ingestion.IngressPassthrough(item, r.defaultSecretNamespace, r.defaultSecretName)...)
		} else {
			m.HTTP = append(m.HTTP, ingestion.Ingress(item, r.defaultSecretNamespace, r.defaultSecretName)...)
		}
	}

	dec, _, _, err := r.sharedTranslator.Translate(m)

	return dec, err
}

func (r *ingressReconciler) createOrUpdateEnvoyConfig(ctx context.Context, desiredCEC *dolphinv1.DolphinEnvoyConfig) error {
	cec := desiredCEC.DeepCopy()

	result, err := controllerutil.CreateOrUpdate(ctx, r.client, cec, func() error {
		cec.Spec = desiredCEC.Spec
		cec.OwnerReferences = desiredCEC.OwnerReferences
		cec.Annotations = mergeMap(cec.Annotations, desiredCEC.Annotations)
		cec.Labels = mergeMap(cec.Labels, desiredCEC.Labels)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update DolphinEnvoyConfig: %w", err)
	}

	r.logger.Debugf("DolphinEnvoyConfig %s has been %s", client.ObjectKeyFromObject(cec), result)

	return nil
}

func (r *ingressReconciler) createOrUpdateService(ctx context.Context, desiredService *corev1.Service) error {
	svc := desiredService.DeepCopy()

	result, err := controllerutil.CreateOrUpdate(ctx, r.client, svc, func() error {
		lbClass := svc.Spec.LoadBalancerClass
		svc.Spec = desiredService.Spec
		svc.Spec.LoadBalancerClass = lbClass

		svc.OwnerReferences = desiredService.OwnerReferences
		svc.Annotations = mergeMap(svc.Annotations, desiredService.Annotations)
		svc.Labels = mergeMap(svc.Labels, desiredService.Labels)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update Service: %w", err)
	}

	r.logger.Debugf("Service %s has been %s", client.ObjectKeyFromObject(svc), result)

	return nil
}

func (r *ingressReconciler) createOrUpdateEndpoints(ctx context.Context, desiredEndpoints *corev1.Endpoints) error {
	ep := desiredEndpoints.DeepCopy()

	result, err := controllerutil.CreateOrUpdate(ctx, r.client, ep, func() error {
		ep.Subsets = desiredEndpoints.Subsets
		ep.OwnerReferences = desiredEndpoints.OwnerReferences
		ep.Annotations = mergeMap(ep.Annotations, desiredEndpoints.Annotations)
		ep.Labels = mergeMap(ep.Labels, desiredEndpoints.Labels)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update Endpoints: %w", err)
	}

	r.logger.Debugf("Endpoints %s has been %s", client.ObjectKeyFromObject(ep), result)

	return nil
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
