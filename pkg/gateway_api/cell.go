package gateway_api

import (
	"context"
	"errors"
	"fmt"

	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlRuntime "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	//myself
	operatorOption "github.com/ccfish2/controller-powered-by-DI/option"
	"github.com/ccfish2/controller-powered-by-DI/pkg/secretsync"

	// dolphin
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
)

var Cell = cell.Module(
	"gateway-api",
	"Manages the Gateway API controllers",

	// initialize configuration
	cell.Config(
		gatewayApiConfig{
			EnableGatewayAPISecretsSync: true,
			GatewayAPISecretsNamespace:  "dolphin-secrets",
		}),
	// start controller
	cell.Invoke(initGatewayAPIController),
	// enable secrets sync
	cell.Provide(registerSecretSync),
)

type gatewayApiConfig struct {
	EnableGatewayAPISecretsSync bool
	GatewayAPISecretsNamespace  string
}

func (r gatewayApiConfig) Flags(flags *pflag.FlagSet) {
	flags.Bool("enable-gateway-api-secrets-sync", r.EnableGatewayAPISecretsSync, "")
	flags.String("gateway-api-secrets-namespace", r.GatewayAPISecretsNamespace, "")
}

var requiredGVK = []schema.GroupVersionKind{
	gatewayv1.SchemeGroupVersion.WithKind("gatewayclasses"),
	gatewayv1.SchemeGroupVersion.WithKind("gateways"),
	gatewayv1.SchemeGroupVersion.WithKind("httproutes"),

	gatewayv1alpha2.SchemeGroupVersion.WithKind("grpcroutes"),
	gatewayv1alpha2.SchemeGroupVersion.WithKind("tlsroutes"),

	gatewayv1beta1.SchemeGroupVersion.WithKind("referencegrants"),
}

type gatewayAPIParams struct {
	cell.In

	Logger             logrus.FieldLogger
	k8sClient          k8sClient.Clientset
	CtrlRuntimeManager ctrlRuntime.Manager
	Scheme             *runtime.Scheme

	Config gatewayApiConfig
}

func initGatewayAPIController(params gatewayAPIParams) error {
	/// check operator EnableGatewayAPI optoin
	if !operatorOption.Config.EnableGatewayAPI {
		return nil
	}

	// check if GatewayAPICRD installed
	params.Logger.WithField("requiredGVK", requiredGVK).Info("checking for required GatewayAPI resources")

	// check if
	if err := checkRequiredCRDs(context.Background(), params.k8sClient); err != nil {
		params.Logger.WithError(err).Error("Required GatewayAPI resources are not found, please refer to docs for instructions")
		return nil
	}
	// register GatewayAPI into API-Scheme
	if err := registerGatewayAPITypesToScheme(params.Scheme); err != nil {
		return err
	}

	// registerReconcilers
	if err := registerReconcilers(
		params.CtrlRuntimeManager,
		params.Config.GatewayAPISecretsNamespace,
		operatorOption.Config.ProxyIdleTimeoutSeconds,
	); err != nil {
		return fmt.Errorf("failed to create gateway controller: %w", err)
	}
	return nil
}

// register the reconcilers one by one into controller manager which handles common tasks
func registerReconcilers(mgr ctrlRuntime.Manager, secretNamespace string, idelTimeoutSeconds int) error {
	reconcilers := []interface {
		SetupWithManager(mgr ctrlRuntime.Manager) error
	}{
		newGatewayClassReconciler(mgr),
		newGatewayReconciler(mgr, secretNamespace, idelTimeoutSeconds),
	}

	for _, r := range reconcilers {
		if err := r.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("failed to setup reconciler: %w", err)
		}
	}
	return nil
}

func checkRequiredCRDs(ctx context.Context, clientset k8sClient.Clientset) error {
	if !clientset.IsEnabled() {
		return nil
	}

	var res error
	for _, gvk := range requiredGVK {
		crd, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, gvk.GroupKind().String(), metav1.GetOptions{})
		if err != nil {
			res = errors.Join(res, err)
			continue
		}
		found := false
		for _, v := range crd.Spec.Versions {
			fmt.Println(v.Name)
			if v.Name == gvk.Version {
				found = true
				break
			}
		}
		if !found {
			res = errors.Join(res, err)
		}
	}
	return res
}

func registerGatewayAPITypesToScheme(scheme *runtime.Scheme) error {
	for gv, f := range map[fmt.Stringer]func(s *runtime.Scheme) error{
		gatewayv1.GroupVersion:       gatewayv1.AddToScheme,
		gatewayv1beta1.GroupVersion:  gatewayv1beta1.AddToScheme,
		gatewayv1alpha2.GroupVersion: gatewayv1beta1.AddToScheme,
	} {
		if err := f(scheme); err != nil {
			return fmt.Errorf("failed to add types from %s to scheme: %w", gv, err)
		}
	}
	return nil
}

// registers the Gateway API for secret synchronization based on TLS secrets referenced
// by a Dolphin Gateway resource
func registerSecretSync(params gatewayAPIParams) secretsync.SecretSyncRegistrationOut {
	// check RequiredCRD
	err := checkRequiredCRDs(context.Background(), params.k8sClient)
	if err != nil {
		return secretsync.SecretSyncRegistrationOut{}
	}

	if operatorOption.Config.EnableGatewayAPI || !params.Config.EnableGatewayAPISecretsSync {
		return secretsync.SecretSyncRegistrationOut{}
	}

	return secretsync.SecretSyncRegistrationOut{
		SecretSyncRegistration: &secretsync.SecretSyncRegistration{
			RefObject:            &gatewayv1.Gateway{},
			RefObjectEnqueueFunc: EnqueueTLSSecrets(params.CtrlRuntimeManager.GetClient(), params.Logger),
			RefobjectCheckFunc:   IsReferencedByDolphinGateway,
			SecretNamespace:      params.Config.GatewayAPISecretsNamespace,
		},
	}
}
