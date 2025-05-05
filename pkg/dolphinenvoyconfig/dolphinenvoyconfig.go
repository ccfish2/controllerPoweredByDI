package dolphinenvoyconfig

import (
	"github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	corev1 "k8s.io/api/core/v1"
)

type envoyconfigReconciler struct {
	client client.Client
	logger logrus.FieldLogger

	l7LoadBalancerAlg   string
	l7LoadBalancerPorts []string
	retries             int
	maxIdleTimeout      int
}

func newenvoyconfigReconciler(c client.Client, logger logrus.FieldLogger, defaultAlgorithm string, ports []string, maxRetries int, idleTimeoutSeconds int) *envoyconfigReconciler {
	return &envoyconfigReconciler{
		c,
		logger,

		defaultAlgorithm,
		ports,
		maxRetries,
		idleTimeoutSeconds,
	}
}

func (r *envoyconfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Using FieldIndex set indexes for envoy config
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&dolphinv1.DolphinEnvoyConfig{}).
		Complete(r)
}
