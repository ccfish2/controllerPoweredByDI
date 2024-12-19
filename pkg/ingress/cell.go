package ingress

import (
	"fmt"

	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	ctrlRuntime "sigs.k8s.io/controller-runtime"
)

var Cell = cell.Module(
	"ingresscontroller",
	"Manages ingress controller",

	cell.Config(
		ingressConfig{
			enableIngressController:             true,
			enforceIngressHTTPs:                 true,
			enableIngressProxyProtocol:          true,
			enableIngressSecretSync:             true,
			ingressSecretNamespace:              "dolphin-secrets",
			ingressLoadBalancerAnnotationPrefix: []string{"service.beta.kubernetes.io", "service.kubernets.io", "cloud.google.com"},
			ingressDefaultLBMode:                "dedicated",
		},
	),
	cell.Invoke(registerReconciler),
)

type ingressConfig struct {
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

func (r ingressConfig) Flags(flags *pflag.FlagSet) {
	flags.StringSlice("ingressLoadBalancerAnnotationPrefix", r.ingressLoadBalancerAnnotationPrefix, "")
	flags.Bool("enableIngressController", r.enableIngressController, "")
	flags.String("ingress-sharedlb-servicename", r.ingressSharedLBServiceName, "")
}

type ingressParams struct {
	cell.In

	Logger logrus.FieldLogger
	Mgr    ctrlRuntime.Manager
	IngCfg ingressConfig
}

func registerReconciler(params ingressParams) error {
	// new one reconcciler
	reconciler := newIngressReconciler(params.Logger, params.Mgr.GetClient(), params.IngCfg.ingressSecretNamespace, params.IngCfg.enforceIngressHTTPs, params.IngCfg.enableIngressProxyProtocol, params.IngCfg.ingressSecretNamespace,
		params.IngCfg.ingressLoadBalancerAnnotationPrefix, params.IngCfg.ingressSecretNamespace, params.IngCfg.ingressDefaultLBMode,
		params.IngCfg.ingressSecretNamespace, params.IngCfg.ingressSecretNamespace, 3)
	// setup the reconciler with manager
	if err := reconciler.SetupWithManager(params.Mgr); err != nil {
		return fmt.Errorf("failed to setup with manager %v", err)
	}

	return nil
}
