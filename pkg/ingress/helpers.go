package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isdolphinManagedIngress(ctx context.Context, c client.Client, log *slog.Logger, ing networkingv1.Ingress) bool {
	ingClsName := ingressClassName(ing)
	if ingClsName != nil {
		log.Info("Checking ingress class", "ingressClassName", *ingClsName, "managedClassName", dolphinIngressClassName)
	}
	if ingClsName != nil && *ingClsName == dolphinIngressClassName {
		log.Info("ingress class is dolphin ingress class")
		return true
	}

	// check if dolphin is default ingress class
	return (ingClsName == nil || *ingClsName == "") && isDolphinDefaultIngressController(ctx, c, log)
}

func isDolphinDefaultIngressController(ctx context.Context, c client.Client, logger *slog.Logger) bool {
	dolphiningressClass := &networkingv1.IngressClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: dolphinIngressClassName}, dolphiningressClass); err != nil {
		if !errors.IsNotFound(err) {
			logger.With("err", err).Error("failed to load dolphin IngressClass")
		}
		return false
	}

	isDefault, err := isIngressClassMarkedAsDefault(*dolphiningressClass)
	if err != nil {
		logger.With("err", err).Error("Failed to detect default class on IngressClass dolphin")
		return false
	}

	return isDefault
}

func ingressClassName(ingress networkingv1.Ingress) *string {
	annotations := ingress.GetAnnotations()
	if className, ok := annotations["kubernetes.io/ingress.class"]; ok {
		return &className
	}

	return ingress.Spec.IngressClassName
}

func isIngressClassMarkedAsDefault(obj networkingv1.IngressClass) (bool, error) {
	if val, ok := obj.GetAnnotations()[networkingv1.AnnotationIsDefaultIngressClass]; ok {
		isDefault, err := strconv.ParseBool(val)
		if err != nil {
			return false, fmt.Errorf("failed to parse annotation %s: %w", networkingv1.AnnotationIsDefaultIngressClass, err)
		}
		return isDefault, nil
	}

	return false, nil
}
