package helpers

import (
	"context"

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
