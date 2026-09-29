package helpers

import (
	"context"

	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ResolveListenerSetToGateway(
	ctx context.Context, c client.Client,
	lsName string, lsNamespace string,
) *types.NamespacedName {
	ls := &gatewayv1.ListenerSet{}
	nn := types.NamespacedName{Namespace: lsNamespace, Name: lsName}
	if err := c.Get(ctx, nn, ls); err != nil {
		return nil
	}

	return ListenerSetParentGateway(ls)
}

func ListenerSetParentGateway(ls *gatewayv1.ListenerSet) *types.NamespacedName {
	gwNamespace := ls.GetNamespace()
	if ls.Spec.ParentRef.Namespace != nil {
		gwNamespace = string(*ls.Spec.ParentRef.Namespace)
	}

	return &types.NamespacedName{
		Namespace: gwNamespace,
		Name:      string(ls.Spec.ParentRef.Name),
	}
}

func HasListenerSetSupport(scheme *runtime.Scheme) bool {
	return scheme.Recognizes(GatewayV1GVK("ListenerSet"))
}

func ListenerEntryToListener(entry gatewayv1.ListenerEntry) gatewayv1.Listener {
	return gatewayv1.Listener(entry)
}

// isListenerSetAllowed determines if a Gateway allows a given ListenerSet
func IsListenerSetAllowed(
	ctx context.Context,
	c client.Client,
	gw *gatewayv1.Gateway,
	ls *gatewayv1.ListenerSet,
	logger *logrus.Entry,
) bool {
	if gw.Spec.AllowedListeners == nil {
		return false
	}
	ns := gw.Spec.AllowedListeners.Namespaces
	if ns == nil || ns.From == nil {
		return false
	}
	switch *ns.From {
	case gatewayv1.NamespacesFromNone:
		return false
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSame:
		return ls.GetNamespace() == gw.GetNamespace()
	case gatewayv1.NamespacesFromSelector:
		nsList := &corev1.NamespaceList{}
		selector, err := metav1.LabelSelectorAsSelector(ns.Selector)
		if err != nil {
			logger.Error(ctx, "Unable to parse namespace selector", logfields.Error, err)
			return false
		}
		if err := c.List(ctx, nsList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			logger.Error(ctx, "Unable to list namespaces", logfields.Error, err)
			return false
		}
		for _, n := range nsList.Items {
			if n.Name == ls.GetNamespace() {
				return true
			}
		}
	}
	return false
}
