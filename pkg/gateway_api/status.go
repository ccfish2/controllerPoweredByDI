package gateway_api

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func merge(existingConditions []metav1.Condition, updates ...metav1.Condition) []metav1.Condition {
	newly := []metav1.Condition{}
	for _, cond := range updates {
		// check if existing conditions has the same type
		// if the same type, check if condition changed
		// if changed update existing[cond] to the updated
		// if it is a new cond, added into existig condition
		found := false
		for j, existing := range existingConditions {
			if existing.Type != cond.Type {
				continue
			}

			if condChanged(cond, existing) {
				existingConditions[j].Status = cond.Status
				existingConditions[j].Message = cond.Message
				existingConditions[j].ObservedGeneration = cond.ObservedGeneration
				existingConditions[j].Reason = cond.Reason
			}
		}
		if !found {
			newly = append(newly, cond)
		}
	}
	existingConditions = append(existingConditions, newly...)
	return existingConditions
}

func condChanged(a, b metav1.Condition) bool {
	return a.Status != b.Status || a.Message != b.Message || a.Reason != b.Reason || a.ObservedGeneration != b.ObservedGeneration
}
