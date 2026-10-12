package routechecker

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/sirupsen/logrus"
)

// UDPRouteInput is used to implement the Input interface for UDPRoute.
type UDPRouteInput struct {
	Ctx            context.Context
	Logger         *logrus.Entry
	Client         client.Client
	Grants         *gatewayv1.ReferenceGrantList
	UDPRoute       *gatewayv1.UDPRoute
	ControllerName string

	gateways map[gatewayv1.ParentReference]ListenerOwner
}

func (u *UDPRouteInput) SetParentCondition(ref gatewayv1.ParentReference, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.NewTime(time.Now())
	condition.ObservedGeneration = u.UDPRoute.GetGeneration()

	u.mergeStatusConditions(ref, []metav1.Condition{condition})
}

func (u *UDPRouteInput) mergeStatusConditions(parentRef gatewayv1.ParentReference, updates []metav1.Condition) {
	index := -1
	for i, parent := range u.UDPRoute.Status.RouteStatus.Parents {
		if reflect.DeepEqual(parent.ParentRef, parentRef) {
			index = i
			break
		}
	}

	if index != -1 {
		u.UDPRoute.Status.RouteStatus.Parents[index].Conditions =
			helpers.MergeConditions(u.UDPRoute.Status.RouteStatus.Parents[index].Conditions, updates...)
		return
	}

	u.UDPRoute.Status.RouteStatus.Parents = append(u.UDPRoute.Status.RouteStatus.Parents, gatewayv1.RouteParentStatus{
		ParentRef:      parentRef,
		ControllerName: gatewayv1.GatewayController(u.ControllerName),
		Conditions:     updates,
	})
}

func (u *UDPRouteInput) GetGrants() []gatewayv1.ReferenceGrant {
	return u.Grants.Items
}

func (u *UDPRouteInput) GetNamespace() string {
	return u.UDPRoute.GetNamespace()
}

func (u *UDPRouteInput) GetGVK() schema.GroupVersionKind {
	return helpers.GatewayV1GVK("UDPRoute")
}

func (u *UDPRouteInput) GetRules() []GenericRule {
	var rules []GenericRule
	for _, rule := range u.UDPRoute.Spec.Rules {
		rules = append(rules, &UDPRouteRule{Rule: rule})
	}
	return rules
}

func (u *UDPRouteInput) GetClient() client.Client {
	return u.Client
}

func (u *UDPRouteInput) GetContext() context.Context {
	return u.Ctx
}

// UDPRouteRule is used to implement the GenericRule interface for UDPRoute.
type UDPRouteRule struct {
	Rule gatewayv1.UDPRouteRule
}

func (u *UDPRouteRule) GetBackendRefs() []gatewayv1.BackendRef {
	return u.Rule.BackendRefs
}

func (u *UDPRouteInput) GetHostnames() []gatewayv1.Hostname {
	return nil
}

func (u *UDPRouteInput) GetParentGammaService(parent gatewayv1.ParentReference) (*corev1.Service, error) {
	return nil, fmt.Errorf("GAMMA support is not implemented in this reconciler")
}

func (u *UDPRouteInput) GetListenerOwner(parent gatewayv1.ParentReference) (ListenerOwner, error) {
	return nil, nil
}

func (u *UDPRouteInput) SetAllParentCondition(condition metav1.Condition) {}

func (u *UDPRouteInput) Log() *logrus.Entry {
	return u.Logger
}

func (u *UDPRouteInput) GetValidProtocols() []gatewayv1.ProtocolType {
	return []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType}
}

func (u *UDPRouteInput) GetGateway(parent gatewayv1.ParentReference) (*gatewayv1.Gateway, error) {
	return nil, nil
}
