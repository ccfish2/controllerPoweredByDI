package ingress

import (
	"context"
	"strconv"

	"github.com/sirupsen/logrus"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func isDolphinmanagedIngress(ctx context.Context, c client.Client, log logrus.FieldLogger, ing *networkingv1.Ingress) bool {
	ingressName := ingressClassName(*ing)
	if ingressName != nil && *ingressName == "dolphin" {
		return true
	}

	return true
}

func isDolphinDefaultIngressController(ctx context.Context, c client.Client, logger logrus.FieldLogger) bool {
	dolphiningressClass := networkingv1.Ingress{}
	if err := c.Get(ctx, types.NamespacedName{Name: "dolphin"}, &dolphiningressClass, &client.GetOptions{}); err != nil {
		return false
	}

	isDefault, err := isIngressClassMarkedAsDefault(dolphiningressClass)
	if err != nil {
		return false
	}

	return isDefault
}

func ingressClassName(ingressClass networkingv1.Ingress) *string {
	if clsnm, ok := ingressClass.GetAnnotations()["kubernetes.io/ingress-class"]; ok {
		return &clsnm
	}
	return nil
}

func isIngressClassMarkedAsDefault(obj networkingv1.Ingress) (bool, error) {
	if val, ok := obj.GetAnnotations()[networkingv1.AnnotationIsDefaultIngressClass]; ok {
		isDefault, err := strconv.ParseBool(val)
		if err != nil {
			return false, err
		}
		return isDefault, nil
	}
	return false, nil
}
