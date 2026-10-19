package gateway_api

import (
	"context"
	"fmt"
	"log/slog"

	helpers "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	routechecks "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/routechecker"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// responsible for route status
type RoutesStsMgr struct {
	client             client.Client
	logger             *slog.Logger
	tcprouteincluded   bool
	udprouteincluded   bool
	tcpsupportedreason string
	udpsupportedreason string
	controllerName     string
}

type RouteStatusInputs struct {
	HttpRoutes     []gatewayv1.HTTPRoute
	TLSRoutes      []gatewayv1.TLSRoute
	GRPCRoutes     []gatewayv1.GRPCRoute
	TCPRoutes      []gatewayv1.TCPRoute
	UDPRoutes      []gatewayv1.UDPRoute
	ReferenceGrant []gatewayv1.ReferenceGrant
}

func NewRouteStatusManager(client client.Client, logger *slog.Logger, controllerName string) *RoutesStsMgr {
	return &RoutesStsMgr{
		client:             client,
		logger:             logger,
		controllerName:     controllerName,
		tcprouteincluded:   true,
		udprouteincluded:   true,
		tcpsupportedreason: "supported",
		udpsupportedreason: "supported",
	}
}

func (m *RoutesStsMgr) SetRouteStatuses(ctx context.Context, scopedLog *slog.Logger, inputs RouteStatusInputs) error {
	if err := m.setHTTPRouteStatuses(ctx, scopedLog, inputs.HttpRoutes, inputs.ReferenceGrant); err != nil {
		return err
	}
	return nil
}

func (m *RoutesStsMgr) setHTTPRouteStatuses(ctx context.Context, scopedLog *slog.Logger, httpRoutes []gatewayv1.HTTPRoute, grants []gatewayv1.ReferenceGrant) error {
	scopedLog.DebugContext(ctx, "Updating HTTPRoute statuses for Gateway", numRoutes, len(httpRoutes))
	scopedLogEntry := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   "setHTTPRouteStatuses",
	})
	for httpRouteIndex, original := range httpRoutes {
		hr := original.DeepCopy()
		hr.Status.Parents = pruneRouteParentStatuses(hr.Status.Parents, hr.Spec.ParentRefs, m.controllerName)

		i := &routechecks.HTTPRouteInput{
			Ctx:            ctx,
			Logger:         scopedLogEntry,
			Client:         m.client,
			Grants:         grants,
			HTTPRoute:      hr,
			ControllerName: m.controllerName,
		}

		if err := m.runCommonRouteChecks(ctx, i, hr.Spec.ParentRefs, hr.Namespace); err != nil {
			return fmt.Errorf("failure during HTTPRoute checks: %w", err)
		}

		for _, validate := range []func() (metav1.Condition, bool){
			i.ValidateHeaderModifier,
			i.ValidateMatchRegexps,
		} {
			if cond, invalid := validate(); invalid {
				for _, parent := range hr.Status.Parents {
					i.SetParentCondition(parent.ParentRef, cond)
				}
			}
		}

		if err := m.updateHTTPRouteStatus(ctx, scopedLog, &original, hr); err != nil {
			return fmt.Errorf("failed to update HTTPRoute status: %w", err)
		}
		httpRoutes[httpRouteIndex].Status = hr.Status
	}
	return nil
}

func (m *RoutesStsMgr) updateHTTPRouteStatus(ctx context.Context, scopedLog *slog.Logger, original *gatewayv1.HTTPRoute, new *gatewayv1.HTTPRoute) error {
	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()
	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, lastTransitionTime)) {
		return nil
	}
	scopedLog.ErrorContext(ctx, "Updating HTTPRoute status", httpRoute, types.NamespacedName{Name: original.Name, Namespace: original.Namespace})
	return m.client.Status().Update(ctx, new)
}

func (m *RoutesStsMgr) parentIsMatchingGateway(ctx context.Context, parent gatewayv1.ParentReference, namespace string) bool {
	hasMatchingControllerFn := helpers.GatewayHasMatchingControllerFn(ctx, m.client, m.controllerName, m.logger)
	if !helpers.IsGateway(parent) {
		return false
	}
	gw := &gatewayv1.Gateway{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Namespace: helpers.NamespaceDerefOr(parent.Namespace, namespace),
		Name:      string(parent.Name),
	}, gw); err != nil {
		return false
	}
	return hasMatchingControllerFn(gw)
}

// checkRouteSupported returns false when route validation should stop for this input.
// this could prevents controller from stalling with further checking
func (m *RoutesStsMgr) checkRouteSupported(input routechecks.Input, parent gatewayv1.ParentReference) bool {
	// switch k := input.GetGVK().Kind; k {
	// case kindTCPRoute, kindUDPRoute:
	// 	if !m.tcprouteincluded && !m.udprouteincluded {
	// 		input.SetParentCondition(parent, metav1.Condition{
	// 			Type:    string(gatewayv1.RouteConditionAccepted),
	// 			Status:  metav1.ConditionFalse,
	// 			Reason:  string(gatewayv1.RouteReasonUnsupportedValue),
	// 			Message: fmt.Sprintf("%s is not supported: %s", k, m.tcpsupportedreason + m.udpsupportedreason),
	// 		})
	// 		input.SetParentCondition(parent, metav1.Condition{
	// 			Type:    string(gatewayv1.RouteConditionResolvedRefs),
	// 			Status:  metav1.ConditionUnknown,
	// 			Reason:  string(gatewayv1.RouteReasonPending),
	// 			Message: fmt.Sprintf("Backend references were not evaluated because this route type is not supported: %s", m.tcpsupportedreason),
	// 		})
	// 		return false
	// 	}
	// }

	return true
}

func (m *RoutesStsMgr) runGatewayRouteChecks(ctx context.Context, input routechecks.Input, parent gatewayv1.ParentReference, objNamespace string) error {
	if !m.parentIsMatchingGateway(ctx, parent, objNamespace) {
		return nil
	}

	if !m.checkRouteSupported(input, parent) {
		return nil
	}

	setInitialRouteConditions(input, parent)

	return nil
}

// runCommonRouteChecks runs all the checks that are common across all supported Route types.
//
// Uses the helpers.Input interface to ensure that this still applies as new types are added.
func (m *RoutesStsMgr) runCommonRouteChecks(ctx context.Context, input routechecks.Input, parentRefs []gatewayv1.ParentReference, objNamespace string) error {
	for _, parent := range parentRefs {
		if helpers.IsGateway(parent) {
			if err := m.runGatewayRouteChecks(ctx, input, parent, objNamespace); err != nil {
				return err
			}
		} else if helpers.IsListenerSet(parent) {
			if err := m.runListenerSetRouteChecks(ctx, input, parent, objNamespace); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *RoutesStsMgr) runListenerSetRouteChecks(ctx context.Context, input routechecks.Input, parent gatewayv1.ParentReference, objNamespace string) error {
	ns := helpers.NamespaceDerefOr(parent.Namespace, objNamespace)
	ls := &gatewayv1.ListenerSet{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Namespace: ns,
		Name:      string(parent.Name),
	}, ls); err != nil {
		return nil
	}

	gwNN := helpers.ListenerSetParentGateway(ls)
	gw := &gatewayv1.Gateway{}
	if err := m.client.Get(ctx, *gwNN, gw); err != nil {
		return nil
	}

	hasMatchingControllerFn := helpers.GatewayHasMatchingControllerFn(ctx, m.client, m.controllerName, m.logger)
	if !hasMatchingControllerFn(gw) {
		return nil
	}

	if !m.checkRouteSupported(input, parent) {
		return nil
	}

	setInitialRouteConditions(input, parent)

	if err := runCheckFuncs(input, parent, gatewayCheckFuncs, "Gateway for ListenerSet"); err != nil {
		return err
	}
	return runCheckFuncs(input, parent, backendCheckFuncs, "Backend for ListenerSet")
}
