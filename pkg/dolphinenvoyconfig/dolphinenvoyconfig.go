package dolphinenvoyconfig

import (
	"github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type envoyconfigReconciler struct {
	client.Client
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
	return nil
}
