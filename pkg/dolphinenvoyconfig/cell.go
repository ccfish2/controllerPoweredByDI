package dolphinenvoyconfig

import (
	operatorOption "github.com/ccfish2/controllerPoweredByDI/option"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"

	ctrl "sigs.k8s.io/controller-runtime"
)

type l7LoadBalancerConfig struct {
	loadbalancerL7Algo string
	loadbalancerl7Port []string
}

var Cell = cell.Module(
	"dolphinenvoyconfig",
	"Manages envoy config controllers",

	cell.Config(l7LoadBalancerConfig{
		"round_robin",
		[]string{},
	}),

	cell.Invoke(registerl7LoadbalancerController),
)

func (p l7LoadBalancerConfig) Flags(pflag *pflag.FlagSet) {
	pflag.String("l7-loadbalancer-algo", p.loadbalancerL7Algo, "speccify the loadblancer algorithm")
	pflag.StringSlice("l7-loadbalancer-ports", p.loadbalancerl7Port, "specify the loadbalancer port")
}

type l7loadbalancerParams struct {
	cell.In

	Logger             logrus.FieldLogger
	CtrlRuntimeManager ctrl.Manager
	Config             l7LoadBalancerConfig
}

func registerl7LoadbalancerController(p l7loadbalancerParams) error {
	if operatorOption.Config.LoadBalancerL7 != "envoy" {
		return nil
	}

	reconciler := newenvoyconfigReconciler(
		p.CtrlRuntimeManager.GetClient(),
		p.Logger,
		p.Config.loadbalancerL7Algo,
		p.Config.loadbalancerl7Port,
		10,
		operatorOption.Config.ProxyIdleTimeoutSeconds,
	)
	if err := reconciler.SetupWithManager(p.CtrlRuntimeManager); err != nil {
		return err
	}
	return nil
}
