package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/util/rand"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/api"
	"github.com/ccfish2/controllerPoweredByDI/auth"
	"github.com/ccfish2/controllerPoweredByDI/endpointgc"
	"github.com/ccfish2/controllerPoweredByDI/identitygc"

	operatork8s "github.com/ccfish2/controllerPoweredByDI/k8s"
	operatorMetrics "github.com/ccfish2/controllerPoweredByDI/metrics"
	operatorOption "github.com/ccfish2/controllerPoweredByDI/option"

	controllerruntime "github.com/ccfish2/controllerPoweredByDI/pkg/controller-runtime"
	"github.com/ccfish2/controllerPoweredByDI/pkg/dolphinendpoint"
	"github.com/ccfish2/controllerPoweredByDI/pkg/dolphinenvoyconfig"
	gatewayapi "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api"
	"github.com/ccfish2/controllerPoweredByDI/pkg/ingress"
	"github.com/ccfish2/controllerPoweredByDI/pkg/libipam"
	"github.com/ccfish2/controllerPoweredByDI/pkg/secretsync"

	// dolphin
	cmtypes "github.com/ccfish2/infra/pkg/clustermesh/types"
	"github.com/ccfish2/infra/pkg/controller"
	"github.com/ccfish2/infra/pkg/defaults"
	"github.com/ccfish2/infra/pkg/gops"
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/hive/job"
	"github.com/ccfish2/infra/pkg/ipam/allocator"
	ipamoptin "github.com/ccfish2/infra/pkg/ipam/option"
	"github.com/ccfish2/infra/pkg/k8s/apis"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	k8sversion "github.com/ccfish2/infra/pkg/k8s/version"
	"github.com/ccfish2/infra/pkg/kvstore/store"
	"github.com/ccfish2/infra/pkg/logging"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/ccfish2/infra/pkg/option"
	"github.com/ccfish2/infra/pkg/pprof"
)

var (
	Operator = cell.Module(
		"operator",
		"Dolphin Operator",

		Infrastructure,
		ControlPlane,
	)

	Infrastructure = cell.Module(
		"operator-infra",
		"Operator Infrastructure",

		pprof.Cell,
		//register pprof HTTP handler, to get runtime metrics
		cell.ProvidePrivate(func(cfg operatorPprofConfig) pprof.Config {
			return pprof.Config{
				Prof:        cfg.OperatorPprof,
				ProfAddress: cfg.OperatorPprofAddress,
				ProfPort:    cfg.OperatorPprofPort,
			}
		}),
		cell.Config(operatorPprofConfig{
			OperatorPprofAddress: operatorOption.PprofAddressOperator,
			OperatorPprofPort:    operatorOption.PprofPortOperator,
		}),

		// runs the gops agent, a tool diagnose go process
		gops.Cell(defaults.GopsPortOperator),

		// provide clientset, API for accessing kubernetes objects
		k8sClient.Cell,

		// modular metrics registry, metric HTTP server
		operatorMetrics.Cell,
		cell.Provide(func(operatorConfig *operatorOption.OperatorConfig) operatorMetrics.SharedConfig {
			return operatorMetrics.SharedConfig{
				// this enablement gets invovle with integration or third party
				EnableMetrics:    operatorConfig.EnableMetrics,
				EnableGatewayAPI: operatorConfig.EnableGatewayAPI,
			}
		}),
	)

	// implements control functions
	ControlPlane = cell.Module(
		"operator-controlplane",
		"Operator Control Plane",

		cell.Config(cmtypes.DefaultClusterInfo),
		cell.Invoke(func(cinfo cmtypes.ClusterInfo) error { return cinfo.InitClusterIDMax() }),
		cell.Invoke(func(cinfo cmtypes.ClusterInfo) error { return cinfo.Validate() }),

		cell.Invoke(registerOperatorHooks),

		// for daemonconfig
		cell.Provide(func() *option.DaemonConfig {
			return option.Config
		}),

		cell.Provide(func() *operatorOption.OperatorConfig {
			return operatorOption.Config
		}),

		cell.Provide(func(
			operatorCfg *operatorOption.OperatorConfig,
			daemonCfg *option.DaemonConfig,
		) identitygc.SharedConfig {
			return identitygc.SharedConfig{
				IdentityAllocationMode: daemonCfg.IdentityAllocationMode,
				K8sNamespace:           daemonCfg.DolphinNamespaceName(),
			}
		}),

		cell.Provide(func(
			daemonCfg *option.DaemonConfig,
		) dolphinendpoint.SharedConfig {
			return dolphinendpoint.SharedConfig{
				EnableDolphinEndpointSlice: daemonCfg.EnableDolphinEndpointSlice,
			}
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

		// api health
		api.HealthHandlerCell(
			kvstoreEnabled,
			isLeader.Load,
		),
		// api metrics
		controller.Cell,

		// operatorapi
		// api
		job.Cell,

		// following cells only init when operator is elected leader
		WithLeaderLifecycle(
			apis.RegisterCRDsCell,
			operatork8s.ResourcesCell,

			libipam.Cell,
			legacyCell,
			auth.Cell,
			store.Cell,

			//
			identitygc.Cell,

			//

			endpointgc.Cell,
			controllerruntime.Cell,
			gatewayapi.Cell,
			ingress.Cell,
			secretsync.Cell,
			dolphinenvoyconfig.Cell,
		),
	)

	binaryName                     = filepath.Base(os.Args[0])
	log                            = logging.DefaultLogger.WithField(logfields.LogSubsys, binaryName)
	leaderElectionResourceLockName = "dolphin-operator-resource-lock"
	// we want to step donw
	leaderElectionCtx       context.Context
	leaderElectionCtxCancel context.CancelFunc
	isLeader                atomic.Bool
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

	option.LogRegisteredOptions(vp, log)
	fmt.Println("Dolphin Operator", "v2.0.0")
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
	isLeader.Store(false)

	leaderElectionCtx, leaderElectionCtxCancel = context.WithCancel(context.Background())

	if clientset.IsEnabled() {
		capabilities := k8sversion.Capabilities()
		log.Info("apabilities <", capabilities, ">")
	}

	operatorID, err := os.Hostname()
	if err != nil {
		log.WithError(err).Fatal("Failed to get hostname when generating lease lock identity")
	}
	operatorID = fmt.Sprintf("%s-%s", operatorID, rand.String(10))

	ns := option.Config.K8sNamespace
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
		10*time.Second)
	if err != nil {
		log.WithError(err).Fatal("Failed to create resource lock for leader election")
	}

	log.Info("Waiting for leader election")
	leaderelection.RunOrDie(leaderElectionCtx, leaderelection.LeaderElectionConfig{
		Name: leaderElectionResourceLockName,

		Lock:            leResourceLock,
		ReleaseOnCancel: true,

		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,

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
				shutdowner.Shutdown(hive.ShutdownWithError(errors.New("leader election lost")))
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

}

func doCleanup() {
	// store false into isLeader
	isLeader.Store(false)
	// execute the leaderelectionCancelFunc
	leaderElectionCtxCancel()
}

func kvstoreEnabled() bool {
	// check kv store configuration enablement
	return true
}

var legacyCell = cell.Invoke(registerlegacyOnLeader)

func registerlegacyOnLeader(lc cell.Lifecycle, k8sclient k8sClient.Clientset, resources operatork8s.Resources, factory store.Factory) {
	ctx, cancel := context.WithCancel(context.Background())
	legacy := legacyOnLeader{
		ctx:       ctx,
		cancel:    cancel,
		clientset: k8sclient,
		resources: resources,
		factory:   factory,
	}
	lc.Append(cell.Hook{
		OnStart: legacy.onStart,
		OnStop:  legacy.onStop,
	})
}

type legacyOnLeader struct {
	ctx       context.Context
	cancel    context.CancelFunc
	clientset k8sClient.Clientset
	wg        sync.WaitGroup
	resources operatork8s.Resources
	factory   store.Factory
}

// Start implements cell.HookInterface.
func (legacy *legacyOnLeader) onStart(_ cell.HookContext) error {
	isLeader.Store(true)
	// check if clientsetenabled
	// checck if DisableDolphinEndpointCRD
	// if unmanagedpodwatcherinterval is specified: spin up one thread safely

	var nodeManager allocator.NodeEventHandler
	var kvstore bool
	var err error
	// init ipam logic

	// check config ipaddress management mode
	// could be azure, aws, multiple pool mode
	// find the provider through allocatorProviders
	// ensure the builtin proviers are not nil
	// invoke alloc.init function
	// cast the alloc to watchers.PooledAllocatorProvider, default implementation
	// start the IPPool Allocator function
	// got one nodeManager obj

	if operatorOption.Config.BGPAnnounceLBIP {
	}

	if kvstoreEnabled() {
	}

	if legacy.clientset.IsEnabled() &&
		(operatorOption.Config.RemoveDolphinNodeTaints && operatorOption.Config.SetDolphinIsUpCondition) {

	}
	dolphinNodeSynchronizer := newDolphinNodeSynchronizer(legacy.clientset, nodeManager, kvstore)
	if legacy.clientset.IsEnabled() {
		err := dolphinNodeSynchronizer.Start(legacy.ctx, &legacy.wg)
		if err != nil {
			return err
		}
	}
	if operatorOption.Config.IPAM == ipamoptin.IPAMClusterPool || operatorOption.Config.IPAM == ipamoptin.IPAMMultiPool {
		// feed IP Pool
	}

	// identitty
	if legacy.clientset.IsEnabled() {
		// some further cleanup
	}
	log.Info("complete initialization")
	return err
}

// Stop implements cell.HookInterface.
func (legacy *legacyOnLeader) onStop(_ cell.HookContext) error {
	legacy.cancel()
	legacy.wg.Wait()
	return nil
}
