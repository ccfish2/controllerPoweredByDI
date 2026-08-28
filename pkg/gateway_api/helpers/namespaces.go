package helpers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	mcsapiv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsapicontrollers "sigs.k8s.io/mcs-api/pkg/controllers"
)

// NamespaceLabelIndex is the map
type NamespaceLabelIndex map[string]map[string]string

func NewNamespaceLabelIndex(namespaces []corev1.Namespace) NamespaceLabelIndex {
	index := make(NamespaceLabelIndex, len(namespaces))
	for _, namespace := range namespaces {
		index[namespace.GetName()] = namespace.GetLabels()
	}
	return index
}

// IsListenerNamespaceAllowed checks whether a route in routeNamespace is
// permitted to attach to the given listener based on AllowedRoutes.Namespaces.
func IsListenerNamespaceAllowed(listener gatewayv1.Listener, routeNamespace, gatewayNamespace string, namespaces NamespaceLabelIndex) bool {
	if listener.AllowedRoutes == nil || listener.AllowedRoutes.Namespaces == nil {
		// Default is Same per Gateway API spec.
		return routeNamespace == gatewayNamespace
	}

	routeNamespaces := listener.AllowedRoutes.Namespaces
	if routeNamespaces.From == nil {
		if routeNamespaces.Selector != nil {
			return isNamespaceSelected(routeNamespaces.Selector, routeNamespace, namespaces)
		}
		// Default is Same per Gateway API spec.
		return routeNamespace == gatewayNamespace
	}

	switch *routeNamespaces.From {
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSame:
		return routeNamespace == gatewayNamespace
	case gatewayv1.NamespacesFromNone:
		return false
	case gatewayv1.NamespacesFromSelector:
		return isNamespaceSelected(routeNamespaces.Selector, routeNamespace, namespaces)
	default:
		return false
	}
}

func isNamespaceSelected(selector *metav1.LabelSelector, routeNamespace string, namespaces NamespaceLabelIndex) bool {
	labelsForNamespace, ok := namespaces[routeNamespace]
	if !ok {
		return false
	}
	selectorMatcher, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}
	return selectorMatcher.Matches(labels.Set(labelsForNamespace))
}

func NamespaceDerefOr(namespace *gatewayv1.Namespace, defaultNamespace string) string {
	if namespace != nil && *namespace != "" {
		return string(*namespace)
	}
	return defaultNamespace
}

func GetBackendServiceName(k8sclient client.Client, namespace string, backendObjectReference gatewayv1.BackendObjectReference) (string, error) {
	backendServiceName := ""
	switch {
	case IsService(backendObjectReference):
		return string(backendObjectReference.Name), nil

	case HasServiceImportSupport(k8sclient.Scheme()) && IsServiceImport(backendObjectReference):
		svcImport := &mcsapiv1alpha1.ServiceImport{}
		if err := k8sclient.Get(context.Background(), client.ObjectKey{
			Namespace: namespace,
			Name:      string(backendObjectReference.Name),
		}, svcImport); err != nil {
			return "", err
		}

		var err error
		backendServiceName, err = GetServiceName(svcImport)
		if err != nil {
			return "", err
		}

	default:
		return "", fmt.Errorf("Unsupported backend kind %s", *backendObjectReference.Kind)
	}

	return backendServiceName, nil
}

// HasServiceImportSupport return if the ServiceImport CRD is supported.
// This checks if the MCS API group is registered in the client scheme
// and it is expected that it is registered only if the ServiceImport
// CRD has been installed prior to the client setup.
func HasServiceImportSupport(scheme *runtime.Scheme) bool {
	return scheme.IsGroupRegistered(mcsapiv1alpha1.GroupVersion.Group)
}

func GetServiceName(svcImport *mcsapiv1alpha1.ServiceImport) (string, error) {
	// ServiceImport gateway api support is conditioned by the fact
	// that an actual Service backs it. Other implementations of MCS API
	// are not supported.
	backendServiceName, ok := svcImport.Annotations[mcsapicontrollers.DerivedServiceAnnotation]
	if !ok {
		return "", fmt.Errorf("%s %s/%s does not have annotation %s", svcImport.Kind, svcImport.Namespace, svcImport.Name, mcsapicontrollers.DerivedServiceAnnotation)
	}

	return backendServiceName, nil
}
