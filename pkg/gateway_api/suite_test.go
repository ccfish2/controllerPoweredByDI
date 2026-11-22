package gateway_api

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	gwscheme "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/scheme"
)

// This is the ONLY suite bootstrap of the package (Ginkgo allows a single
// BeforeSuite / AfterSuite). One envtest API server is started for all specs,
// with every CRD the controllers need installed.

var (
	cfg              *rest.Config
	k8sClientforTest client.Client // used by gatewayclass_controller_test.go
	testEnv          *envtest.Environment

	// used by the data driven cases (cases_test.go / harness_test.go): knows
	// all gateway-api, dolphin and cilium kinds
	caseScheme *runtime.Scheme
	caseClient client.Client
)

// CRDs, relative to the package directory.
var (
	crdGatewayAPIDir   = filepath.Join("testdata", "crd", "gateway-api")
	crdDolphinEnvoyCfg = filepath.Join("testdata", "crd", "dolphinenvoyconfigs.yaml")
	crdCiliumEnvoyCfg  = filepath.Join("testdata", "crd", "ciliumenvoyconfigs.yaml")
)

// findEnvtestBinaries returns the directory that contains kube-apiserver/etcd.
// Order: $KUBEBUILDER_ASSETS, then any pkg/gateway_api/bin/*1.37* directory.
func findEnvtestBinaries() string {
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		return dir
	}
	matches, err := filepath.Glob(filepath.Join(".", "bin", "*1.37*"))
	if err == nil {
		for _, m := range matches {
			if _, statErr := os.Stat(filepath.Join(m, "kube-apiserver")); statErr == nil {
				return m
			}
		}
	}
	// Fall back to the original hard-coded location.
	return filepath.Join(".", "bin", "envtest-v1.37.0-darwin-amd64")
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment (all CRDs)")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			crdGatewayAPIDir,
			crdDolphinEnvoyCfg,
			crdCiliumEnvoyCfg,
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: findEnvtestBinaries(),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// client for the GatewayClass specs
	err = gatewayv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClientforTest, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientforTest).NotTo(BeNil())

	// client for the data driven cases
	caseScheme = gwscheme.TestScheme(helpers.AllOptionalKinds)
	caseClient, err = client.New(cfg, client.Options{Scheme: caseScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(caseClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cleanupMkcert()
	Expect(testEnv.Stop()).To(Succeed())
})
