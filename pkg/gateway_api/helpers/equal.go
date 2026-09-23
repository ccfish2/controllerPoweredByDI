package helpers

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ObjectsEqual(a, b client.Object) (bool, error) {
	if a.GetObjectKind().GroupVersionKind() == b.GetObjectKind().GroupVersionKind() &&
		a.GetName() == b.GetName() &&
		a.GetNamespace() == b.GetNamespace() {
		// Same object, need to check generation.

		if a.GetGeneration() == b.GetGeneration() {
			// If generations are the same, then the objects are equal
			return true, nil
		}

		return true, errors.New("Same object, different generations, retry reconciliation")
	}

	return false, nil
}

// ContainsCommonHTTPRoute checks to see if the two slices of HTTPRoutes contain
// at least one identical HTTPRoute. If so, returns true.
//
// Returns an error if the two lists contain a HTTPRoute that is the same object
// with a different generation; this means there has been a HTTPRoute update
// between when the two lists were generated, and the whole reconciliation must be
// restarted.
func ContainsCommonHTTPRoute(a, b []gatewayv1.HTTPRoute) (bool, error) {
	for _, hrA := range a {
		for _, hrB := range b {
			same, err := ObjectsEqual(&hrA, &hrB)
			if err != nil {
				return true, err
			}
			if same {
				return true, nil
			}
		}
	}
	return false, nil
}
