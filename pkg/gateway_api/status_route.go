package gateway_api

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

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

func (m *RoutesStsMgr) SetRouteStatuses(
	ctx context.Context,
	scopedLog *slog.Logger,
	inputs RouteStatusInputs,
) (retErr error) {
	logger := scopedLog
	if logger == nil {
		logger = slog.Default()
	}

	logger.InfoContext(ctx, "SetRouteStatuses entered",
		"http_route_count", len(inputs.HttpRoutes),
		"reference_grant_count", len(inputs.ReferenceGrant),
	)
	// keep this for routecheckFn
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.ErrorContext(ctx, "PANIC in SetRouteStatuses",
				"panic", recovered,
				"stack", string(debug.Stack()),
			)
			panic(recovered)
		}

		logger.InfoContext(ctx, "SetRouteStatuses exiting",
			"error", retErr,
		)
	}()

	if m == nil {
		return fmt.Errorf("SetRouteStatuses: RoutesStsMgr is nil")
	}

	if err := m.setHTTPRouteStatuses(
		ctx, logger, inputs.HttpRoutes, inputs.ReferenceGrant,
	); err != nil {
		logger.ErrorContext(ctx, "setHTTPRouteStatuses returned error",
			"error", err,
		)
		return err
	}

	logger.InfoContext(ctx, "SetRouteStatuses completed successfully")
	return nil
}

func (m *RoutesStsMgr) setHTTPRouteStatuses(
	ctx context.Context,
	scopedLog *slog.Logger,
	httpRoutes []gatewayv1.HTTPRoute,
	grants []gatewayv1.ReferenceGrant,
) error {
	logger := scopedLog
	if logger == nil {
		logger = slog.Default()
	}

	logger.InfoContext(ctx, "setHTTPRouteStatuses entered",
		"route_count", len(httpRoutes),
		"grant_count", len(grants),
		"controller_name", m.controllerName,
	)

	scopedLogEntry := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   "setHTTPRouteStatuses",
	})

	if len(httpRoutes) == 0 {
		logger.InfoContext(ctx, "No HTTPRoutes to update")
		return nil
	}

	for routeIndex, original := range httpRoutes {
		routeLog := logger.With(
			"route_index", routeIndex,
			"route_namespace", original.Namespace,
			"route_name", original.Name,
			"route_uid", string(original.UID),
		)

		routeLog.InfoContext(ctx, "Processing HTTPRoute",
			"parent_ref_count", len(original.Spec.ParentRefs),
			"existing_parent_status_count", len(original.Status.Parents),
		)

		hr := original.DeepCopy()

		routeLog.DebugContext(ctx, "Pruning existing parent statuses")
		hr.Status.Parents = pruneRouteParentStatuses(
			hr.Status.Parents,
			hr.Spec.ParentRefs,
			m.controllerName,
		)

		routeLog.DebugContext(ctx, "Parent statuses pruned",
			"remaining_parent_status_count", len(hr.Status.Parents),
		)

		i := &routechecks.HTTPRouteInput{
			Ctx:            ctx,
			Logger:         scopedLogEntry,
			Client:         m.client,
			Grants:         grants,
			HTTPRoute:      hr,
			ControllerName: m.controllerName,
		}

		routeLog.InfoContext(ctx, "Running common HTTPRoute checks")
		if err := m.runCommonRouteChecks(
			ctx, i, hr.Spec.ParentRefs, hr.Namespace,
		); err != nil {
			routeLog.ErrorContext(ctx, "Common HTTPRoute checks failed",
				"error", err,
			)
			return fmt.Errorf(
				"common checks failed for HTTPRoute %s/%s: %w",
				hr.Namespace, hr.Name, err,
			)
		}
		routeLog.InfoContext(ctx, "Common HTTPRoute checks completed",
			"parent_status_count", len(hr.Status.Parents),
		)

		validators := []struct {
			name string
			fn   func() (metav1.Condition, bool)
		}{
			{name: "ValidateHeaderModifier", fn: i.ValidateHeaderModifier},
			{name: "ValidateMatchRegexps", fn: i.ValidateMatchRegexps},
		}

		for _, validator := range validators {
			routeLog.DebugContext(ctx, "Running HTTPRoute validator",
				"validator", validator.name,
			)

			cond, invalid := validator.fn()
			if !invalid {
				routeLog.DebugContext(ctx, "HTTPRoute validator passed",
					"validator", validator.name,
				)
				continue
			}

			routeLog.WarnContext(ctx, "HTTPRoute validator found invalid configuration",
				"validator", validator.name,
				"condition_type", cond.Type,
				"condition_status", cond.Status,
				"condition_reason", cond.Reason,
				"parent_status_count", len(hr.Status.Parents),
			)

			for _, parent := range hr.Status.Parents {
				i.SetParentCondition(parent.ParentRef, cond)
			}
		}

		routeLog.InfoContext(ctx, "Updating HTTPRoute status in API",
			"parent_status_count", len(hr.Status.Parents),
		)

		if err := m.updateHTTPRouteStatus(ctx, logger, &original, hr); err != nil {
			routeLog.ErrorContext(ctx, "HTTPRoute status update failed",
				"error", err,
			)
			return fmt.Errorf(
				"failed to update HTTPRoute status for %s/%s: %w",
				hr.Namespace, hr.Name, err,
			)
		}

		routeLog.InfoContext(ctx, "HTTPRoute status update returned successfully")

		// Preserve the existing behavior of updating the caller's slice.
		httpRoutes[routeIndex].Status = hr.Status

		routeLog.DebugContext(ctx, "Copied computed status into route slice",
			"parent_status_count", len(httpRoutes[routeIndex].Status.Parents),
		)
	}

	logger.InfoContext(ctx, "setHTTPRouteStatuses completed",
		"processed_route_count", len(httpRoutes),
	)

	return nil
}

func (m *RoutesStsMgr) updateHTTPRouteStatus(
	ctx context.Context,
	scopedLog *slog.Logger,
	original *gatewayv1.HTTPRoute,
	new *gatewayv1.HTTPRoute,
) error {
	routeKey := types.NamespacedName{
		Name:      original.Name,
		Namespace: original.Namespace,
	}

	oldStatus := original.Status.DeepCopy()
	newStatus := new.Status.DeepCopy()

	scopedLog.InfoContext(ctx, "Comparing HTTPRoute statuses",
		"route", routeKey,
		"old_parent_count", len(oldStatus.Parents),
		"new_parent_count", len(newStatus.Parents),
		"old_generation", original.Generation,
		"new_generation", new.Generation,
	)

	if cmp.Equal(oldStatus, newStatus, cmpopts.IgnoreFields(metav1.Condition{}, lastTransitionTime)) {
		scopedLog.InfoContext(ctx, "Skipping HTTPRoute status update: status unchanged",
			"route", routeKey,
		)
		return nil
	}

	scopedLog.InfoContext(ctx, "Calling Kubernetes API to update HTTPRoute status",
		"route", routeKey,
	)

	err := m.client.Status().Update(ctx, new)
	if err != nil {
		scopedLog.ErrorContext(ctx, "Kubernetes HTTPRoute status update failed",
			"route", routeKey,
			"error", err,
		)
		return fmt.Errorf("update status for HTTPRoute %s: %w", routeKey, err)
	}

	scopedLog.InfoContext(ctx, "Kubernetes HTTPRoute status update succeeded",
		"route", routeKey,
	)

	return nil
}

func (m *RoutesStsMgr) parentIsMatchingGateway(
	ctx context.Context,
	parent gatewayv1.ParentReference,
	namespace string,
) bool {
	if !helpers.IsGateway(parent) {
		m.logger.InfoContext(ctx, "Route parent is not a Gateway",
			"parent_name", string(parent.Name),
			"parent_namespace", helpers.NamespaceDerefOr(parent.Namespace, namespace),
		)
		return false
	}

	ns := helpers.NamespaceDerefOr(parent.Namespace, namespace)
	key := types.NamespacedName{
		Namespace: ns,
		Name:      string(parent.Name),
	}

	m.logger.InfoContext(ctx, "Checking whether route parent Gateway is controlled by this controller",
		"gateway", key,
		"controller_name", m.controllerName,
	)

	hasMatchingControllerFn := helpers.GatewayHasMatchingControllerFn(
		ctx, m.client, m.controllerName, m.logger,
	)

	gw := &gatewayv1.Gateway{}
	if err := m.client.Get(ctx, key, gw); err != nil {
		m.logger.WarnContext(ctx, "Unable to fetch parent Gateway; skipping route checks",
			"gateway", key,
			"error", err,
		)
		return false
	}

	m.logger.InfoContext(ctx, "Fetched parent Gateway",
		"gateway", key,
		"gateway_controller_name", gw.Spec.GatewayClassName,
		"generation", gw.Generation,
	)

	matches := hasMatchingControllerFn(gw)

	m.logger.InfoContext(ctx, "Parent Gateway controller ownership check completed",
		"gateway", key,
		"matches_controller", matches,
		"controller_name", m.controllerName,
	)

	return matches
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

func (m *RoutesStsMgr) runGatewayRouteChecks(
	ctx context.Context,
	input routechecks.Input,
	parent gatewayv1.ParentReference,
	objNamespace string,
) error {
	matches := m.parentIsMatchingGateway(ctx, parent, objNamespace)
	if !matches {
		m.logger.InfoContext(ctx, "Skipping Gateway route checks: parent is not controlled by this controller",
			"parent_name", string(parent.Name),
			"route_namespace", objNamespace,
		)
		return nil
	}

	if !m.checkRouteSupported(input, parent) {
		m.logger.InfoContext(ctx, "Skipping Gateway route checks: route type is unsupported",
			"parent_name", string(parent.Name),
		)
		return nil
	}

	m.logger.InfoContext(ctx, "Setting initial route conditions",
		"parent_name", string(parent.Name),
		"route_namespace", objNamespace,
	)

	setInitialRouteConditions(input, parent)

	m.logger.InfoContext(ctx, "Initial route conditions set",
		"parent_name", string(parent.Name),
	)

	return nil
}

// runCommonRouteChecks runs all the checks that are common across all supported Route types.
// Uses the helpers.Input interface to ensure that this still applies as new types are added.
func (m *RoutesStsMgr) runCommonRouteChecks(
	ctx context.Context,
	input routechecks.Input,
	parentRefs []gatewayv1.ParentReference,
	objNamespace string,
) error {
	m.logger.InfoContext(ctx, "Starting common route checks",
		"route_namespace", objNamespace,
		"parent_ref_count", len(parentRefs),
		"route_kind", input.GetGVK().Kind,
	)

	for idx, parent := range parentRefs {
		m.logger.InfoContext(ctx, "Processing route parent reference",
			"parent_index", idx,
			"parent_name", string(parent.Name),
			"parent_namespace", helpers.NamespaceDerefOr(parent.Namespace, objNamespace),
			"parent_kind", fmt.Sprint(parent.Kind),
		)

		switch {
		case helpers.IsGateway(parent):
			m.logger.InfoContext(ctx, "Route parent identified as Gateway")

			if err := m.runGatewayRouteChecks(ctx, input, parent, objNamespace); err != nil {
				m.logger.ErrorContext(ctx, "Gateway route checks failed",
					"parent_name", string(parent.Name),
					"error", err,
				)
				return err
			}

		case helpers.IsListenerSet(parent):
			m.logger.InfoContext(ctx, "Route parent identified as ListenerSet")

			if err := m.runListenerSetRouteChecks(ctx, input, parent, objNamespace); err != nil {
				m.logger.ErrorContext(ctx, "ListenerSet route checks failed",
					"parent_name", string(parent.Name),
					"error", err,
				)
				return err
			}

		default:
			m.logger.WarnContext(ctx, "Skipping unsupported route parent kind",
				"parent_name", string(parent.Name),
				"parent_kind", fmt.Sprint(parent.Kind),
			)
		}
	}

	m.logger.InfoContext(ctx, "Common route checks finished",
		"route_namespace", objNamespace,
	)

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
	// I skip this purposedly
	// if err := runCheckFuncs(input, parent, gatewayCheckFuncs, "Gateway for ListenerSet"); err != nil {
	// 	return err
	// }
	// return runCheckFuncs(input, parent, backendCheckFuncs, "Backend for ListenerSet")
	return nil
}
