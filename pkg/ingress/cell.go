package ingress

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	ctrlRuntime "sigs.k8s.io/controller-runtime"
)

var Cell = cell.Module(
	"ingresscontroller",
	"Manages ingress controller",

	cell.Config(
		IngressConfig{
			enableIngressController:             true,
			enforceIngressHTTPs:                 true,
			enableIngressProxyProtocol:          true,
			enableIngressSecretSync:             true,
			ingressSecretNamespace:              "dolphin-secrets",
			ingressLoadBalancerAnnotationPrefix: []string{"service.beta.kubernetes.io", "service.kubernets.io", "cloud.google.com"},
			ingressDefaultLBMode:                "dedicated",
		},
	),
	cell.Provide(registerReconciler),
)

type IngressConfig struct {
	enableIngressController             bool
	enforceIngressHTTPs                 bool
	enableIngressProxyProtocol          bool
	enableIngressSecretSync             bool
	ingressSecretNamespace              string
	ingressLoadBalancerAnnotationPrefix []string
	ingressSharedLBServiceName          string
	ingressDefaultLBMode                string
	IngressDefaultSecretNamespace       string
	IngressDefaultSecretName            string
}

func (ingCfg IngressConfig) Flags(flags *pflag.FlagSet) {
	panic("unimpl")
}

type ingressParams struct {
	cell.In

	logger logrus.FieldLogger
	mgr    ctrlRuntime.Manager
	ingCfg IngressConfig
}

func registerReconciler(params ingressParams) error {
	// new one reconcciler
	ingr := newIngressReconciler(params.logger, params.mgr.GetClient(), params.ingCfg.ingressSecretNamespace, params.ingCfg.enforceIngressHTTPs, params.ingCfg.enableIngressProxyProtocol, params.ingCfg.ingressSecretNamespace,
		params.ingCfg.ingressLoadBalancerAnnotationPrefix, params.ingCfg.ingressSecretNamespace, params.ingCfg.ingressDefaultLBMode,
		params.ingCfg.ingressSecretNamespace, params.ingCfg.ingressSecretNamespace, 3)
	// setup the reconciler with manager
	if err := ingr.SetupWithManager(params.mgr); err != nil {
		return err
	}
	return nil
}
