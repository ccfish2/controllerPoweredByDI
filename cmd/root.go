package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/ccfish2/infra/pkg/controller"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/util/rand"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/endpointgc"
	operatorK8s "github.com/ccfish2/controllerPoweredByDI/k8s"
	operatorMetrics "github.com/ccfish2/controllerPoweredByDI/metrics"
	operatorOption "github.com/ccfish2/controllerPoweredByDI/option"
	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	gatewayapi "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api"
	"github.com/ccfish2/controllerPoweredByDI/pkg/libipam"
	"github.com/ccfish2/controllerPoweredByDI/pkg/secretsync"

	// dolphin
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s/apis"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	k8sversion "github.com/ccfish2/infra/pkg/k8s/version"
	"github.com/ccfish2/infra/pkg/logging"
	"github.com/ccfish2/infra/pkg/option"
)

var (
	Operator = cell.Module(
		"operator",
		"Dolphin Operator",
		Infrastructure,
		ControllPlane,
	)

	Infrastructure = cell.Module(
		"operator-infra",
		"Operator Infrastructure",

		// todo: register the pprof HTTP handler, get golang profiling data

		// API for access kubernetes client
		k8sClient.Cell,

		cell.Provide(func(operatorCfg *operatorOption.OperatorConfig,
		) operatorMetrics.SharedConfig {
			return operatorMetrics.SharedConfig{
				EnableMetrics:    operatorCfg.EnableMetrics,
				EnableGatewayAPI: operatorCfg.EnableGatewayAPI,
			}
		}),
	)

	// implements control plane functionalities
	ControllPlane = cell.Module(
		"operator-controlplane",
		"Operator Control Plane",

		// todo: register cluster info need cluster network configuration

		cell.Invoke(registerOperatorHooks),

		cell.Provide(func() *option.DaemonConfig {
			return option.Config
		}),

		cell.Provide(func() *operatorOption.OperatorConfig {
			return operatorOption.Config
		}),

		cell.Provide(func(
			operatorCfg *operatorOption.OperatorConfig,
			daemonCfg *option.DaemonConfig,
		) endpointgc.SharedConfig {
			return endpointgc.SharedConfig{
				Interval:                  operatorCfg.EndpointGCInterval,
				DisableDolphinEndpointCRD: daemonCfg.DisableDolphinEndpointCRD,
			}
		}),

		controller.Cell,
		//operatorApi.SpecCell,
		//api.ServerCell,

		// Provides a global job registry which cells can use to spawn job groups.
		//job.Cell,

		WithLeaderLifecycle(
			apis.RegisterCRDsCell,
			operatorK8s.ResourcesCell,

			libipam.Cell,
			//auth.Cell,
			//store.Cell,
			//legacyCell,

			//identitygc.Cell,

			// Dolphin Endpoint Garbage Collector. It removes all leaked Dolphin
			// Endpoints. Either once or periodically it validates all the present
			// Dolphin Endpoints and delete the ones that should be deleted.
			endpointgc.Cell,

			// Integrates the controller-runtime library and provides its components via Hive.
			controllerruntime.Cell,
			gatewayapi.Cell,
			secretsync.Cell,
			// Dolphin L7 LoadBalancing with Envoy.
			//dolphinenvoyconfig.Cell,
		),
	)

	binaryName = filepath.Base(os.Args[0])

	log = logging.DefaultLogger.WithField(logfields.LogSubsys, binaryName)

	leaderElectionResourceLockName = "dolphin-operator-resource-lock"

	// we want to step donw
	leaderElectionCtx       context.Context
	leaderElectionCtxCancel context.CancelFunc

	isLeader atomic.Bool
)

func NewOperatorCmd(h *hive.Hive) *cobra.Command {
	cmd := &cobra.Command{
		Use:   binaryName,
		Short: "Run " + binaryName,
		Run: func(cobraCmd *cobra.Command, args []string) {
			cmdRefDir := h.Viper().GetString("cmdref")
			if cmdRefDir != "" {
				fmt.Println("generating some command reference")
				os.Exit(0)
			}

			initEnv(h.Viper())

			if err := h.Run(); err != nil {
				panic(err)
			}
		},
	}

	h.RegisterFlags(cmd.Flags())
	cmd.AddCommand(
		MetricsCmd,
		h.Command(),
	)

	return cmd
}

func initEnv(vp *viper.Viper) {
	option.Config.Populate(vp)
	operatorOption.Config.Populate(vp)

	if err := logging.SetupLogging(option.Config.LogDriver, logging.LogOptions(option.Config.LogOpt), binaryName, option.Config.Debug); err != nil {
		panic(err)
	}

	option.LogRegisteredOptions(vp, nil)
	fmt.Println("Dolphin Operator", "v1.0.0")
}

func Execute(cmd *cobra.Command) {
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func registerOperatorHooks(lc cell.Lifecycle, llc *LeaderLifecycle, clientset k8sClient.Clientset, shutdowner hive.Shutdowner) {
	var wg sync.WaitGroup
	lc.Append(cell.Hook{
		OnStart: func(cell.HookContext) error {
			wg.Add(1)
			go func() {
				runOperator(llc, clientset, shutdowner)
				wg.Done()
			}()
			return nil
		},
		OnStop: func(ctx cell.HookContext) error {
			if err := llc.Stop(ctx); err != nil {
				return err
			}
			doCleanup()
			wg.Wait()
			return nil
		},
	})
}

func runOperator(lc *LeaderLifecycle, clientset k8sClient.Clientset, shutdowner hive.Shutdowner) {
	fmt.Println("runOperator")
	isLeader.Store(false)

	leaderElectionCtx, leaderElectionCtxCancel = context.WithCancel(context.Background())

	if clientset.IsEnabled() {
		capabilities := k8sversion.Capabilities()
		log.Info("apabilities %v", capabilities)
		// if !capabilities.MinimalVersionMet {
		// 	log.Fatalf("Minimal kubernetes version not met: %s < %s",
		// 		k8sversion.Version(), k8sversion.MinimalVersionConstraint)
		// }
	}

	// We only support Operator in HA mode for Kubernetes Versions having support for
	// LeasesResourceLock.
	// See docs on capabilities.LeasesResourceLock for more context.
	// if !k8sversion.Capabilities().LeasesResourceLock {
	// 	log.Info("Support for coordination.k8s.io/v1 not present, fallback to non HA mode")

	// 	if err := lc.Start(leaderElectionCtx); err != nil {
	// 		log.WithError(err).Fatal("Failed to start leading")
	// 	}
	// 	return
	// }

	// Get hostname for identity name of the lease lock holder.
	// We identify the leader of the operator cluster using hostname.
	operatorID, err := os.Hostname()
	if err != nil {
		log.WithError(err).Fatal("Failed to get hostname when generating lease lock identity")
	}
	operatorID = fmt.Sprintf("%s-%s", operatorID, rand.String(10))

	ns := option.Config.K8sNamespace
	// If due to any reason the CILIUM_K8S_NAMESPACE is not set we assume the operator
	// to be in default namespace.
	if ns == "" {
		ns = metav1.NamespaceDefault
	}

	leResourceLock, err := resourcelock.NewFromKubeconfig(
		resourcelock.LeasesResourceLock,
		ns,
		leaderElectionResourceLockName,
		resourcelock.ResourceLockConfig{
			// Identity name of the lock holder
			Identity: operatorID,
		},
		clientset.RestConfig(),
		operatorOption.Config.LeaderElectionRenewDeadline)
	if err != nil {
		log.WithError(err).Fatal("Failed to create resource lock for leader election")
	}

	// Start the leader election for running cilium-operators
	log.Info("Waiting for leader election")
	leaderelection.RunOrDie(leaderElectionCtx, leaderelection.LeaderElectionConfig{
		Name: leaderElectionResourceLockName,

		Lock:            leResourceLock,
		ReleaseOnCancel: true,

		LeaseDuration: operatorOption.Config.LeaderElectionLeaseDuration,
		RenewDeadline: operatorOption.Config.LeaderElectionRenewDeadline,
		RetryPeriod:   operatorOption.Config.LeaderElectionRetryPeriod,

		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				if err := lc.Start(ctx); err != nil {
					log.WithError(err).Error("Failed to start when elected leader, shutting down")
					shutdowner.Shutdown(hive.ShutdownWithError(err))
				}
			},
			OnStoppedLeading: func() {
				log.WithField("operator-id", operatorID).Info("Leader election lost")
				// Cleanup everything here, and exit.
				shutdowner.Shutdown(hive.ShutdownWithError(errors.New("Leader election lost")))
			},
			OnNewLeader: func(identity string) {
				if identity == operatorID {
					log.Info("Leading the operator HA deployment")
				} else {
					log.WithFields(logrus.Fields{
						"newLeader":  identity,
						"operatorID": operatorID,
					}).Info("Leader re-election complete")
				}
			},
		},
	})

	// invoke leaderelection.RunOrDie(leaderElectionCtx, leaderelection.LeaderElectionConfig)
	// name
	// Lock
	// ReleaseOnCancel

	// https://pkg.go.dev/k8s.io/client-go@v0.29.2/tools/leaderelection
}

func doCleanup() {
	// store false into isLeader
	isLeader.Store(false)
	// execute the leaderelectionCancelFunc
	leaderElectionCtxCancel()
}
