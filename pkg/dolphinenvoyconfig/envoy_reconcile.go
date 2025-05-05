package dolphinenvoyconfig

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	dolphinEnvoyLBPrefix = "dolphin-envoy-lb"
)

func (r *envoyconfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// scoped log
	scopedLog := r.logger.WithFields(logrus.Fields{
		logfields.Controller: "dolphinenvoyconfig",
		logfields.Resource:   req.NamespacedName,
	})
	scopedLog.Info("Starting reconcilation")
	// retrieve svc from the namespace
	svc := &corev1.Service{}
	if err := r.client.Get(ctx, req.NamespacedName, svc); err != nil {
		if k8serrors.IsNotFound(err) {
			scopedLog.Debug("Service not found")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}
	// isLBAnnotationEnabled
	if isLBProtocolAnnotationEnabled(svc) || hasAnyPort(svc, r.l7LoadBalancerPorts) {
		if err := r.createOrUpdateDolphinEnvoyConfig(ctx, svc); err != nil {
			scopedLog.WithError(err).Error("Failed to create or update DolphinEnvoyConfig")
			return ctrl.Result{}, err
		}
	} else {
		if err := r.deleteEnvoyConfig(ctx, svc); err != nil {
			scopedLog.WithError(err).Error("Failed to delete DolphinEnvoyConfig")
			return ctrl.Result{}, err
		}
	}
	scopedLog.Info("Reconcilation completed successfully")
	// createOrUpdateEnvoyConfig
	return ctrl.Result{}, nil
}

func hasAnyPort(svc *corev1.Service, ports []string) bool {
	for _, p := range ports {
		for _, port := range svc.Spec.Ports {
			if p == getServiceFrontendPort(port) {
				return true
			}
		}
	}
	return false
}

func getServiceFrontendPort(port corev1.ServicePort) string {
	if port.Port != 0 {
		return strconv.Itoa(int(port.Port))
	}
	if port.NodePort != 0 {
		return strconv.Itoa(int(port.NodePort))
	}
	return port.Name
}

func (r *envoyconfigReconciler) createOrUpdateDolphinEnvoyConfig(ctx context.Context, svc *corev1.Service) error {
	desired, err := r.getEnvoyConfigFroService(svc)
	if err != nil {
		return fmt.Errorf("failed to get DolphinEnvoyConfig for service: %w", err)
	}

	if err := controllerutil.SetControllerReference(svc, desired, r.client.Scheme()); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	exists := true
	existing := dolphinv1.DolphinEnvoyConfig{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, &existing); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf(" failed to lookup DolphinEnvoyConfig: %w", err)
		}
		exists = false
	}

	scopedLog := r.logger.WithField("svcKey", getName(svc))
	if exists {
		if desired.DeepEqual(&existing) {
			r.logger.WithField("dolphinEnvoyConfigName", fmt.Sprintf("%s/%s", desired.Namespace, desired.Name)).Debug("No change for existing DolphinEnvoyConfig")
			return nil
		}

		// Update existing CEC
		updated := existing.DeepCopy()
		updated.Spec = desired.Spec

		scopedLog.Debug("Updating DolphinEnvoyConfig")
		if err := r.client.Update(ctx, updated); err != nil {
			return fmt.Errorf("failed to update DolphinEnvoyConfig for service: %w", err)
		}

		scopedLog.Debug("Updated DolphinEnvoyConfig for service")
		return nil
	}

	scopedLog.Debug("Creating DolphinEnvoyConfig")
	if err := r.client.Create(ctx, desired); err != nil {
		return fmt.Errorf("failed to create DolphinEnvoyConfig for service: %w", err)
	}

	scopedLog.Debug("Created DolphinEnvoyConfig for service")
	return nil
}

func (r *envoyconfigReconciler) deleteEnvoyConfig(ctx context.Context, svc *corev1.Service) error {
	existing := dolphinv1.DolphinEnvoyConfig{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: fmt.Sprintf("%s-%s", "dolphin-envoy-lb", svc.Name)}, &existing); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to lookup DolphinEnvoyConfig: %w", err)
		}
		return nil
	}

	r.logger.Debug("Deleting DolphinEnvoyConfig")
	if err := r.client.Delete(ctx, &existing); err != nil {
		return fmt.Errorf("failed to delete DolphinEnvoyConfig for service: %w", err)
	}

	return nil
}
