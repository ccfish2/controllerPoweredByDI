package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/cilium/cilium/pkg/metrics"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vbatts/tar-split/version"
	"google.golang.org/appengine/log"

	// myself
	"github.com/ccfish2/controller-powered-by-DI/api"

	operatorMetrics "github.com/ccfish2/controller-powered-by-DI/metrics"
	operatorOption "github.com/ccfish2/controller-powered-by-DI/option"
	gateway_api "github.com/ccfish2/controller-powered-by-DI/pkg/gateway-api"

	// dolphin
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	operatorK8s "github.com/ccfish2/infra/pkg/k8s"
	"github.com/ccfish2/infra/pkg/k8s/apis"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
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

		cell.Invoke(func() *option.DaemonConfig {
			return option.Config
		}),

		cell.Provide(func() *operatorOption.OperatorConfig {
			return operatorOption.Config
		}),

		//controller.Cell,
		api.ServerCell,

		//job.Cell,

		WithLeaderLifecycle(
			apis.RegisterCRDsCell,
			operatorK8s.ResourcesCell,
			gateway_api.Cell,
		),
	)

	binaryName = filepath.Base(os.Args[0])

	leaderElectionResourceLockName = "dolphin-operator-resource-lock"

	// Use a Go context so we can tell the leaderelection code when
	// we want to step donw
	leaderElectionCtx       context.Context
	leaderElectionCtxCancel context.CancelFunc

	// isLeader is an atomic boolean value that is true when the Operator is
	// elected leader. Otherwise, it is false
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

	// Enable fallback to direct API probing to check for support of Leases in
	// case Discovery API fails

	cmd.AddCommand(
		MetricsCmd,
		h.Command(),
	)

	return cmd
}

func initEnv(vp *viper.Viper) {
	option.Config.Populate(vp)
	operatorOption.Config.Populate(vp)

	logging.DefaultLogger.Hooks.Add(metrics.NewLoggingHook())

	if err := logging.SetupLogging(option.Config.LogDriver, logging.LogOptions(option.Config.LogOpt), binaryName, option.Config.Debug); err != nil {
		panic("err")
	}

	option.logRegisteredOptions(vp, log)
	log.Infof("Dolphin Operator %s", version.VERSION)
}

func Execute(cmd *cobra.Command) {
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
