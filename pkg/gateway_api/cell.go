package gateway_api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/ccfish2/infra/pkg/backoff"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/logging/logfields"
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
	operatorOption "github.com/ccfish2/controllerPoweredByDI/option"
	"github.com/ccfish2/controllerPoweredByDI/pkg/secretsync"

	// dolphin
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	mcsapiv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
)

const crdDiscoveryTimeout = 30 * time.Second

var requiredGVKs = []schema.GroupVersionKind{
	gatewayv1.SchemeGroupVersion.WithKind(helpers.GatewayClassKind),
	gatewayv1.SchemeGroupVersion.WithKind(helpers.GatewayKind),
	gatewayv1.SchemeGroupVersion.WithKind(helpers.HTTPRouteKind),
	gatewayv1.SchemeGroupVersion.WithKind(helpers.GRPCRouteKind),
	gatewayv1beta1.SchemeGroupVersion.WithKind(helpers.ReferenceGrantKind),
}

var optionalGVKs = []schema.GroupVersionKind{
	gatewayv1alpha2.SchemeGroupVersion.WithKind(helpers.TLSRouteKind),
	mcsapiv1alpha1.SchemeGroupVersion.WithKind(helpers.ServiceImportKind),
}

// Cell manages gateway api controllers
var Cell = cell.Module(
	"gateway-api",
	"Manages the Gateway API controllers",

	// initialize configuration
	cell.Provide(func() *slog.Logger {
		return slog.New(slog.NewTextHandler(os.Stdout, nil))
	}),
	cell.Config(
		gatewayApiConfig{
			EnableGatewayAPISecretsSync: true,
			GatewayAPISecretsNamespace:  "dolphin-secrets",
			EnableGatewayAPI:            true,
		}),

	cell.ProvidePrivate(newGatewayAPIPreconditions),
	// start controller
	cell.Invoke(initGatewayAPIController),
	// enable secrets sync
	cell.Provide(registerSecretSync),
)

// preconditionParams contains dependencies for checking Gateway API preconditions.
type preconditionParams struct {
	cell.In

	Logger           *slog.Logger
	K8sClient        k8sClient.Clientset
	Health           cell.Health
	OperatorConfig   *operatorOption.OperatorConfig
	GatewayApiConfig gatewayApiConfig
}

// newGatewayAPIPreconditions checks all Gateway API preconditions and returns
// the result. This includes config checks, kube-proxy-replacement check,
// external traffic policy validation, and CRD discovery with retry logic.
func newGatewayAPIPreconditions(params preconditionParams) (*gatewayAPIPreconditions, error) {
	if !operatorOption.Config.EnableGatewayAPI {
		return &gatewayAPIPreconditions{Enabled: false}, nil
	}

	if !params.OperatorConfig.KubeProxyReplacement {
		params.Logger.Warn("Gateway API support requires kube-proxy-replacement enabled")
		return &gatewayAPIPreconditions{Enabled: false}, nil
	}

	if err := validateExternalTrafficPolicy(params.GatewayApiConfig, params.Logger); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), crdDiscoveryTimeout)
	defer cancel()

	return discoverCRDsWithRetry(ctx, params.K8sClient, params.Logger, params.Health)
}

// discoverCRDsWithRetry attempts to discover required Gateway API CRDs, retrying
// on transient errors until the context expires. Returns a fatal error if the
// context expires (causing operator restart) or disables Gateway API gracefully
// if CRDs are permanently missing.
func discoverCRDsWithRetry(ctx context.Context, client k8sClient.Clientset, logger *slog.Logger, health cell.Health) (*gatewayAPIPreconditions, error) {
	logger.Info(
		"Checking for required and optional GatewayAPI resources",
		logfields.RequiredGVK, requiredGVKs,
		logfields.OptionalGVK, optionalGVKs,
	)

	// Configure exponential backoff for CRD discovery.
	// This allows the operator to recover if API server is temporarily unavailable at startup.
	bo := backoff.Exponential{
		Logger: logger,
		Min:    200 * time.Millisecond,
		Max:    5 * time.Second,
		Factor: 2.0,
		Jitter: true,
		Name:   "gateway-api-crd-discovery",
	}

	for {
		installedKinds, err := checkCRDs(ctx, client, logger, requiredGVKs, optionalGVKs)
		if err == nil {
			// health.OK("Gateway API CRDs discovered")
			return &gatewayAPIPreconditions{
				Enabled:        true,
				InstalledKinds: installedKinds,
			}, nil
		}

		// Context expiry errors from checkCRDs bypass isTransientError, silently
		// disabling Gateway API. Catch them here to trigger an operator restart.
		if ctx.Err() != nil {
			logger.Error(
				"Gateway API CRD discovery timed out after retrying transient errors, operator will restart",
				logfields.Error, err,
			)
			// health.Stopped("Gateway API CRD discovery timed out after transient errors")
			return nil, fmt.Errorf("gateway API CRD discovery timed out: %w", err)
		}

		if !isTransientError(err) {
			logger.Error(
				"Required GatewayAPI resources are not found, please refer to docs for installation instructions",
				logfields.Error, err,
			)
			// health.Degraded("Gateway API CRDs not installed", err)
			return &gatewayAPIPreconditions{Enabled: false}, nil
		}

		logger.Warn(
			"Failed to check GatewayAPI CRDs due to transient error, will retry",
			logfields.Error, err,
		)
		// health.Degraded("Gateway API initialization pending - API server unreachable", err)

		if err := bo.Wait(ctx); err != nil {
			// Context expired during backoff wait - same handling as above.
			logger.Error(
				"Gateway API CRD discovery timed out after retrying transient errors, operator will restart",
				logfields.Error, err,
			)
			// health.Stopped("Gateway API CRD discovery timed out after transient errors")
			return nil, fmt.Errorf("gateway API CRD discovery timed out: %w", err)
		}
	}
}

// checkCRDs checks if required and optional CRDs are present in the cluster,
// returns an error if the required CRDs are not installed, and returns the
// schema.GroupVersionKind of any optional CRDs that are installed.
func checkCRDs(ctx context.Context, clientset k8sClient.Clientset, logger *slog.Logger, requiredGVKs, optionalGVKs []schema.GroupVersionKind) ([]schema.GroupVersionKind, error) {
	var res error
	var presentGVKs []schema.GroupVersionKind

	for _, gvk := range requiredGVKs {
		if err := checkCRD(ctx, clientset, gvk); err != nil {
			res = errors.Join(res, err)
		}
	}

	for _, optionalGVK := range optionalGVKs {
		if err := checkCRD(ctx, clientset, optionalGVK); err != nil {
			logger.DebugContext(ctx, "CRD is not present, will not handle it", logfields.OptionalGVK, optionalGVK)
			continue
		}
		// note that the .Kind field contains the _resource_ name -
		// the plural, lowercase version of the name.
		presentGVKs = append(presentGVKs, optionalGVK)
	}

	return presentGVKs, res
}

func checkCRD(ctx context.Context, clientset k8sClient.Clientset, gvk schema.GroupVersionKind) error {
	if !clientset.IsEnabled() {
		return nil
	}

	crd, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, gvk.GroupKind().String(), metav1.GetOptions{})
	if err != nil {
		return err
	}

	found := false
	for _, v := range crd.Spec.Versions {
		if v.Name == gvk.Version {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("CRD %q does not have version %q", gvk.GroupKind().String(), gvk.Version)
	}

	return nil
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}

	// Check for Kubernetes API server errors that are transient
	if k8serrors.IsServerTimeout(err) ||
		k8serrors.IsServiceUnavailable(err) ||
		k8serrors.IsTooManyRequests(err) ||
		k8serrors.IsTimeout(err) {
		return true
	}

	// Check for network-level errors (connection refused, reset, host unreachable)
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EHOSTUNREACH, syscall.ENETUNREACH:
			return true
		}
	}

	return false
}

func validateExternalTrafficPolicy(cfg gatewayApiConfig, logger *slog.Logger) error {
	return nil
}

type gatewayApiConfig struct {
	EnableGatewayAPISecretsSync bool   `mapstructure:"enable-gateway-api-secrets-sync,omitempty"`
	GatewayAPISecretsNamespace  string `mapstructure:"gateway-api-secrets-namespace,omitempty"`
	EnableGatewayAPI            bool   `mapstructure:"enable-gateway-api,omitempty"`
}

func (r gatewayApiConfig) Flags(flags *pflag.FlagSet) {
	flags.BoolVar(&r.EnableGatewayAPISecretsSync, "enable-gateway-api-secrets-sync", false, "")
	flags.StringVar(&r.GatewayAPISecretsNamespace, "gateway-api-secrets-namespace", "dolphin", "")
	flags.BoolVar(&r.EnableGatewayAPI, "enable-gateway-api", true, "")
}

var requiredGVK = []schema.GroupVersionKind{
	gatewayv1.SchemeGroupVersion.WithKind("gatewayclasses"),
	gatewayv1.SchemeGroupVersion.WithKind("gateways"),
	gatewayv1.SchemeGroupVersion.WithKind("httproutes"),
	gatewayv1beta1.SchemeGroupVersion.WithKind("referencegrants"),
	gatewayv1.SchemeGroupVersion.WithKind("grpcroutes"),
	gatewayv1alpha2.SchemeGroupVersion.WithKind("tlsroutes"),
}

type gatewayAPIParams struct {
	cell.In

	Logger             logrus.FieldLogger
	K8sClient          k8sClient.Clientset
	CtrlRuntimeManager ctrlRuntime.Manager
	Scheme             *runtime.Scheme

	Config gatewayApiConfig
	// Preconditions is injected from private provider
	Preconditions *gatewayAPIPreconditions
}

// gatewayAPIPreconditions holds the result of Gateway API precondition checks.
// This is provided privately and consumed by both initGatewayAPIController
// and registerSecretSync to ensure consistent behavior.
type gatewayAPIPreconditions struct {
	// Enabled indicates all preconditions are met for Gateway API
	Enabled bool
	// InstalledKinds contains the GVKs of installed Gateway API CRDs
	InstalledKinds []schema.GroupVersionKind
}

func initGatewayAPIController(params gatewayAPIParams) error {
	/// check operator EnableGatewayAPI optoin
	if !params.Config.EnableGatewayAPI {
		log.Info("Gateway api is not enabled. Skip registering GatewayAPI controllers")
		return nil
	}
	// check if GatewayAPICRD installed
	params.Logger.WithField("requiredGVK", requiredGVK).Info("checking for required GatewayAPI resources")

	// check if
	if err := checkRequiredCRDs(context.Background(), params.K8sClient); err != nil {
		params.Logger.WithError(err).Error("Required GatewayAPI resources are not found, please refer to docs for instructions")
		return nil
	}

	installedKinds := params.Preconditions.InstalledKinds

	// registerReconcilers
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := registerReconcilers(
		params.CtrlRuntimeManager,
		params.Config.GatewayAPISecretsNamespace,
		operatorOption.Config.ProxyIdleTimeoutSeconds,
		logger,
		installedKinds,
	); err != nil {
		return fmt.Errorf("failed to create gateway controller: %w", err)
	}
	return nil
}

// register the reconcilers one by one into controller manager which handles common tasks
func registerReconcilers(mgr ctrlRuntime.Manager, secretNamespace string, idelTimeoutSeconds int, logger *slog.Logger, installedCRDs []schema.GroupVersionKind) error {
	reconcilers := []interface {
		SetupWithManager(mgr ctrlRuntime.Manager) error
	}{
		newGatewayClassReconciler(mgr),
		newGatewayReconciler(mgr, secretNamespace, idelTimeoutSeconds, true, false, logger, installedCRDs),
		newhttpRouteReconciler(mgr),
		newGRPCRouteReconciler(mgr),
		newtlsrouteReconciler(mgr),
	}

	for _, r := range reconcilers {
		if err := r.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("failed to setup reconciler %#v: %w", r, err)
		}
	}
	log.Info("Gateway API controllers registered successfully")
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
		for _, version := range crd.Spec.Versions {
			if version.Name == gvk.Version && version.Served {
				found = true
				break
			}
		}

		if !found {
			res = errors.Join(res, fmt.Errorf(
				"CRD %s does not serve version %s",
				crd.Name,
				gvk.Version,
			))
		}
	}
	return res
}

// registers the Gateway API for secret synchronization based on TLS secrets referenced
// by a Dolphin Gateway resource
func registerSecretSync(params gatewayAPIParams) secretsync.SecretSyncRegistrationOut {
	// check RequiredCRD
	err := checkRequiredCRDs(context.Background(), params.K8sClient)
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
			RefObjectCheckFunc:   IsReferencedByDolphinGateway,
			SecretsNamespace:     params.Config.GatewayAPISecretsNamespace,
		},
	}
}
