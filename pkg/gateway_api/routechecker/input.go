package routechecker

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// controllerName is the gateway controller name used in dolphin
	controllerName = "io.dolphin/gateway-controller"
)

type Input interface {
	GetGateway() (*gatewayv1.Gateway, error)
	GetHostName() []gatewayv1.Hostname
	GetClient() client.Client
	GetContext() context.Context
	GetNamespace() string

	SetParentCondition(parentRef gatewayv1.ParentReference, cond metav1.Condition)
	SetParentAllCondition(cond metav1.Condition)
}

type GenericRule interface {
	GetBackendRefs() []gatewayv1.BackendRef
}
