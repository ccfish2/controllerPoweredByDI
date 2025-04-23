package ingress

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isdolphinManagedIngress(ctx context.Context, c client.Client, log logrus.FieldLogger, ing networkingv1.Ingress) bool {
	ingressName := ingressClassName(ing)
	if ingressName != nil && *ingressName == "dolphin" {
		return true
	}

	// check if dolphin is default ingress class
	return (ingressName == nil || *ingressName == "") && isDolphinDefaultIngressController(ctx, c, log)
}

func isDolphinDefaultIngressController(ctx context.Context, c client.Client, logger logrus.FieldLogger) bool {
	dolphiningressClass := &networkingv1.IngressClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: "dolphin"}, dolphiningressClass); err != nil {
		if !errors.IsNotFound(err) {
			logger.WithError(err).Error("failed to load dolphin ingress class")
		}
		return false
	}

	isDefault, err := isIngressClassMarkedAsDefault(*dolphiningressClass)
	if err != nil {
		logger.WithError(err).Error("failed to check if ingress class is marked as default")
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
