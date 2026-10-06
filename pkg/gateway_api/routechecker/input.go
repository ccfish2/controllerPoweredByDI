package routechecker

import (
	"context"

	"github.com/sirupsen/logrus"
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
