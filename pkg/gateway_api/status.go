package gateway_api

import (
	"reflect"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func newCondition(conditionType string, status metav1.ConditionStatus, reason, msg string, lastTransitionTime time.Time, observedGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.NewTime(lastTransitionTime),
		ObservedGeneration: observedGeneration,
	}
}

// merge combines the provided conditions with the existing conditions.
func merge(existingConditions []metav1.Condition, updates ...metav1.Condition) []metav1.Condition {
	var additions []metav1.Condition
	for i, update := range updates {
		found := false
		for j, cond := range existingConditions {
			if cond.Type == update.Type {
				found = true
				if conditionChanged(cond, update) {
					existingConditions[j].Status = update.Status
					existingConditions[j].Reason = update.Reason
					existingConditions[j].Message = update.Message
					existingConditions[j].ObservedGeneration = update.ObservedGeneration
					existingConditions[j].LastTransitionTime = update.LastTransitionTime
				}
				break
			}
		}
		if !found {
			additions = append(additions, updates[i])
		}
	}
	existingConditions = append(existingConditions, additions...)
	return existingConditions
}

func conditionChanged(a, b metav1.Condition) bool {
	return a.Status != b.Status ||
		a.Reason != b.Reason ||
		a.Message != b.Message ||
		a.ObservedGeneration != b.ObservedGeneration
}

func pruneRouteParentStatuses(parents []gatewayv1.RouteParentStatus, currentParentRefs []gatewayv1.ParentReference, controllerName string) []gatewayv1.RouteParentStatus {
	filtered := parents[:0]
	for _, parentStatus := range parents {
		if string(parentStatus.ControllerName) != controllerName || slices.ContainsFunc(currentParentRefs, func(ref gatewayv1.ParentReference) bool {
			return reflect.DeepEqual(ref, parentStatus.ParentRef)
		}) {
			filtered = append(filtered, parentStatus)
		}
	}
	return filtered
}
