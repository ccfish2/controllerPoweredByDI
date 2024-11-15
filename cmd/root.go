package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	// myself
	operatorK8s "github.com/ccfish2/controller-powered-by-DI/k8s"
	operatorMetrics "github.com/ccfish2/controller-powered-by-DI/metrics"
	operatorOption "github.com/ccfish2/controller-powered-by-DI/option"
	gateway_api "github.com/ccfish2/controller-powered-by-DI/pkg/gateway-api"

	// dolphin
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
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
		WithLeaderLifecycle(
			apis.RegisterCRDsCell,
			operatorK8s.ResourcesCell,
			gateway_api.Cell,
		),
	)

	binaryName = filepath.Base(os.Args[0])

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
		panic("err")
	}

	option.LogRegisteredOptions(vp, nil)
	fmt.Println("Dolphin Operator %s", "v1.0.0")
}

func Execute(cmd *cobra.Command) {
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
