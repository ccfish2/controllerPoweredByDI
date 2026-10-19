package routechecker

import (
	"context"
	"fmt"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// controllerName is the gateway controller name used in dolphin
	controllerName = "io.dolphin/gateway-controller"
)

type Input interface {
	GetRules() []GenericRule
	GetNamespace() string
	GetClient() client.Client
	GetContext() context.Context
	GetGVK() schema.GroupVersionKind
	GetGrants() []gatewayv1.ReferenceGrant
	GetGateway(parent gatewayv1.ParentReference) (*gatewayv1.Gateway, error)
	GetHostnames() []gatewayv1.Hostname
	GetListenerOwner(parent gatewayv1.ParentReference) (ListenerOwner, error)
	GetValidProtocols() []gatewayv1.ProtocolType

	SetParentCondition(ref gatewayv1.ParentReference, condition metav1.Condition)
	SetAllParentCondition(condition metav1.Condition)
	Log() *logrus.Entry
}

type GenericRule interface {
	GetBackendRefs() []gatewayv1.BackendRef
}

type CheckRuleFunc func(input Input) (bool, error)
type CheckGatewayFunc func(input Input, ref gatewayv1.ParentReference) (bool, error)

type ListenerOwner interface {
	GetListeners() []gatewayv1.Listener
	GetNamespace() string
}

type GatewayListenerOwner struct {
	*gatewayv1.Gateway
}

type (
	CheckWithParentFunc func(input Input, ref gatewayv1.ParentReference) (bool, error)
)

func (g *GatewayListenerOwner) GetListeners() []gatewayv1.Listener {
	return g.Spec.Listeners
}

type ListenerSetListenerOwner struct {
	Listeners []gatewayv1.Listener
	Namespace string
}

func (l *ListenerSetListenerOwner) GetListeners() []gatewayv1.Listener {
	return l.Listeners
}

func (l *ListenerSetListenerOwner) GetNamespace() string {
	return l.Namespace
}

func ResolveListenerOwner(
	ctx context.Context,
	c client.Client,
	parent gatewayv1.ParentReference,
	defaultNamespace string,
) (ListenerOwner, error) {
	ns := helpers.NamespaceDerefOr(parent.Namespace, defaultNamespace)

	if helpers.IsListenerSet(parent) {
		ls := &gatewayv1.ListenerSet{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(parent.Name)}, ls); err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("error while getting listenerset: %w", err)
			}
			return nil, fmt.Errorf("listenerset %q does not exist: %w", parent.Name, err)
		}

		listeners := make([]gatewayv1.Listener, 0, len(ls.Spec.Listeners))
		for _, entry := range ls.Spec.Listeners {
			listeners = append(listeners, helpers.ListenerEntryToListener(entry))
		}
		return &ListenerSetListenerOwner{
			Listeners: listeners,
			Namespace: ls.GetNamespace(),
		}, nil
	}

	gw := &gatewayv1.Gateway{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(parent.Name)}, gw); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("error while getting gateway: %w", err)
		}
		return nil, fmt.Errorf("gateway %q does not exist: %w", parent.Name, err)
	}
	return &GatewayListenerOwner{Gateway: gw}, nil
}
