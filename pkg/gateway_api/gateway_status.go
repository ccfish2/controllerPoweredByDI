package gateway_api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	gatewayClassAcceptedMessage    = "Valid GatewayClass"
	gatewayClassNotAcceptedMessage = "Invalid Gateway Class"
)

func setGatewayAccepted(gw *gatewayv1.Gateway, accepted bool, msg string) *gatewayv1.Gateway {
	gw.Status.Conditions = merge(gw.Status.Conditions, gwStsAcpCondition(gw, accepted, msg))
	return gw
}

func gatewayListenerAcceptedCondition(gw *gatewayv1.Gateway, ready bool, msg string) metav1.Condition {
	switch ready {
	case true:
		return metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerConditionAccepted),
			ObservedGeneration: gw.Generation,
			LastTransitionTime: metav1.NewTime(metav1.Now().Time),
			Message:            msg,
		}
	default:
		return metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.ListenerReasonPending),
			ObservedGeneration: gw.Generation,
			LastTransitionTime: metav1.NewTime(metav1.Now().Time),
			Message:            msg,
		}
	}

}

func gwStsAcpCondition(gw *gatewayv1.Gateway, accepted bool, msg string) metav1.Condition {
	switch accepted {
	case true:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayClassReasonAccepted),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	default:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionAccepted),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.GatewayReasonNoResources),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	}
}
func setGatewayProgrammed(gw *gatewayv1.Gateway, ready bool, msg string) *gatewayv1.Gateway {
	gw.Status.Conditions = merge(gw.Status.Conditions, gwStsProgrmCondition(gw, ready, msg))
	return gw
}

func gwStsProgrmCondition(gw *gatewayv1.Gateway, ready bool, msg string) metav1.Condition {
	switch ready {
	case true:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayClassReasonAccepted),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	default:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionProgrammed),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.GatewayReasonListenersNotReady),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	}
}

func gatewayListenerInvalidRouteKinds(gw *gatewayv1.Gateway, msg string) metav1.Condition {
	return metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.ListenerReasonInvalidRouteKinds),
		Message:            msg,
		ObservedGeneration: gw.GetGeneration(),
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

func gatewayListenerProgrammedCondition(gw *gatewayv1.Gateway, ready bool, msg string) metav1.Condition {
	switch ready {
	case true:
		return metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerConditionProgrammed),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	default:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayConditionProgrammed),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.ListenerReasonPending),
			Message:            msg,
			ObservedGeneration: gw.GetGeneration(),
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
	}

}

func setGatewayClassAccepted(gwc *gatewayv1.GatewayClass, accepted bool) *gatewayv1.GatewayClass {
	gwc.Status.Conditions = merge(gwc.Status.Conditions, gatewayClassAcceptedCondition(gwc, accepted))
	return gwc
}

func gatewayClassAcceptedCondition(gwc *gatewayv1.GatewayClass, accepted bool) metav1.Condition {
	switch accepted {
	case true:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Reason:             string(gatewayv1.GatewayClassReasonAccepted),
			Message:            gatewayClassAcceptedMessage,
		}
	default:
		return metav1.Condition{
			Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.NewTime(time.Now()),
			Reason:             string(gatewayv1.GatewayClassReasonPending),
			Message:            gatewayClassNotAcceptedMessage,
		}
	}
}
