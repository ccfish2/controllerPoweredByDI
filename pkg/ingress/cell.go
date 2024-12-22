package ingress

import (
	"fmt"

	"github.com/ccfish2/controllerPoweredByDI/pkg/secretsync"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	networkingv1 "k8s.io/api/networking/v1"

	ctrlRuntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var Cell = cell.Module(
	"ingress",
	"Manages ingress controller",

	cell.Config(
		ingressConfig{
			EnableIngressController:     false,
			EnforceIngressHTTPS:         true,
			EnableIngressProxyProtocol:  true,
			EnableIngressSecretsSync:    true,
			IngressSecretsNamespace:     "dolphin-secrets",
			IngressLBAnnotationPrefixes: []string{"service.beta.kubernetes.io", "service.kubernets.io", "cloud.google.com"},
			IngressDefaultLBMode:        "dedicated",
		},
	),
	cell.Invoke(registerReconciler),
	cell.Provide(registerSecretSync),
)

type ingressConfig struct {
	EnableIngressController       bool
	EnforceIngressHTTPS           bool
	EnableIngressProxyProtocol    bool
	EnableIngressSecretsSync      bool
	IngressSecretsNamespace       string
	IngressLBAnnotationPrefixes   []string
	IngressSharedLBServiceName    string
	IngressDefaultLBMode          string
	IngressDefaultSecretNamespace string
	IngressDefaultSecretName      string
}

func (r ingressConfig) Flags(flags *pflag.FlagSet) {
	flags.Bool("enable-ingress-controller", r.EnableIngressController, "Enables dolphin ingress controller. This must be enabled along with enable-envoy-config in dolphin agent.")
	flags.Bool("enforce-ingress-https", r.EnforceIngressHTTPS, "Enforces https for host having matching TLS host in Ingress. Incoming traffic to http listener will return 308 http error code with respective location in header.")
	flags.Bool("enable-ingress-proxy-protocol", r.EnableIngressProxyProtocol, "Enable proxy protocol for all Ingress listeners. Note that _only_ Proxy protocol traffic will be accepted once this is enabled.")
	flags.Bool("enable-ingress-secrets-sync", r.EnableIngressSecretsSync, "Enables fan-in TLS secrets from multiple namespaces to singular namespace (specified by ingress-secrets-namespace flag)")
	flags.String("ingress-secrets-namespace", r.IngressSecretsNamespace, "Namespace having tls secrets used by Ingress and CEC.")
	flags.StringSlice("ingress-lb-annotation-prefixes", r.IngressLBAnnotationPrefixes, "Annotations and labels which are needed to propagate from Ingress to the Load Balancer.")
	flags.String("ingress-shared-lb-service-name", r.IngressSharedLBServiceName, "Name of shared LB service name for Ingress.")
	flags.String("ingress-default-lb-mode", r.IngressDefaultLBMode, "Default loadbalancer mode for Ingress. Applicable values: dedicated, shared")
	flags.String("ingress-default-secret-namespace", r.IngressDefaultSecretNamespace, "Default secret namespace for Ingress.")
	flags.String("ingress-default-secret-name", r.IngressDefaultSecretName, "Default secret name for Ingress.")
}

type ingressParams struct {
	cell.In

	Logger logrus.FieldLogger
	Mgr    ctrlRuntime.Manager
	IngCfg ingressConfig
}

func registerReconciler(params ingressParams) error {
	// new one reconcciler
	reconciler := newIngressReconciler(params.Logger, params.Mgr.GetClient(), params.IngCfg.IngressSecretsNamespace, params.IngCfg.EnforceIngressHTTPS, params.IngCfg.EnableIngressProxyProtocol, params.IngCfg.IngressSecretsNamespace,
		params.IngCfg.IngressLBAnnotationPrefixes, params.IngCfg.IngressSecretsNamespace, params.IngCfg.IngressDefaultLBMode,
		params.IngCfg.IngressSecretsNamespace, params.IngCfg.IngressSecretsNamespace, 3)
	// setup the reconciler with manager
	if err := reconciler.SetupWithManager(params.Mgr); err != nil {
		return fmt.Errorf("failed to setup with manager %v", err)
	}

	return nil
}

// register the ingress controller for secret synchronization for tls referenced by ingress resources
func registerSecretSync(params ingressParams) secretsync.SecretSyncRegistrationOut {
	if !params.IngCfg.EnableIngressController || params.IngCfg.EnableIngressSecretsSync {
		return secretsync.SecretSyncRegistrationOut{}
	}

	registration := secretsync.SecretSyncRegistrationOut{
		SecretSyncRegistration: &secretsync.SecretSyncRegistration{
			RefObject:            &networkingv1.Ingress{},
			RefObjectEnqueueFunc: EnqueueReferencedTLSSecrets(params.Mgr.GetClient(), params.Logger),
			RefObjectCheckFunc:   IsReferencedByDolphinIngress,
			SecretsNamespace:     params.IngCfg.IngressSecretsNamespace,
			AdditionalWatches: []secretsync.AdditionalWatch{
				{
					RefObject:            &networkingv1.IngressClass{},
					RefObjectEnqueueFunc: enqueueAllSecrets(params.Mgr.GetClient()),
					RefObjectWatchOptions: []builder.WatchesOption{
						builder.WithPredicates(predicate.AnnotationChangedPredicate{}),
					},
				},
			},
		},
	}

	// check secret name and namespace
	return registration
}
