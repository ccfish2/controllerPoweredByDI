package gateway_api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func setGatewayAccepted(gw *gatewayv1.Gateway, accepted bool, msg string) *gatewayv1.Gateway {
	gw.Status.Conditions = merge(gw.Status.Conditions, gwStsAcpCondition(gw, accepted, msg))
	return gw
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
