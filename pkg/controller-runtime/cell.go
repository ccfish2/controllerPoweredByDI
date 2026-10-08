package controllerruntime

import (
	"context"
	"fmt"
	"runtime/pprof"

	logrusr "github.com/bombsimon/logrusr/v4"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/hive/job"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	gatewayapischeme "github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/scheme"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	dolphinv2alpha1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v2alpha1"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
)

var Cell = cell.Module(
	"controller-runtime",
	"Manages the controller-runtime integration and its components",
	cell.Provide(NewScheme),
	cell.Provide(NewManager),
)

func NewScheme() (*runtime.Scheme, error) {
	scheme := clientgoscheme.Scheme

	for gv, f := range map[fmt.Stringer]func(s *runtime.Scheme) error{
		dolphinv1.SchemeGroupVersion: dolphinv1.AddToScheme,
	} {
		if err := f(scheme); err != nil {
			return nil, fmt.Errorf("%V", gv)
		}
	}

	for gv, f := range map[fmt.Stringer]func(s *runtime.Scheme) error{
		dolphinv2alpha1.SchemeGroupVersion: dolphinv2alpha1.AddToScheme,
	} {
		if err := f(scheme); err != nil {
			return nil, fmt.Errorf("%V", gv)
		}
	}

	if err := gatewayapischeme.AddToScheme(scheme); err != nil {
		return nil, err
	}

	return scheme, nil
}

type mgrParams struct {
	cell.In

	Loggger     logrus.FieldLogger
	Lifecycle   cell.Lifecycle
	JobRegistry job.Registry
	Scope       cell.Scope

	K8sClient k8sclient.Clientset
	Scheme    *runtime.Scheme
}

func NewManager(params mgrParams) (ctrlruntime.Manager, error) {
	if !params.K8sClient.IsEnabled() {
		return nil, fmt.Errorf("k8s client is not enabled")
	}

	equality.Semantic.AddFunc(func(rs1, rs2 dolphinv1.XDSResource) bool {
		return proto.Equal(rs1.Any, rs2.Any)
	})

	ctrlruntime.SetLogger(logrusr.New(params.Loggger))
	mgr, err := ctrlruntime.NewManager(params.K8sClient.RestConfig(), ctrlruntime.Options{
		Scheme: params.Scheme,
		Metrics: metricserver.Options{
			BindAddress: "0",
		},
		Logger: logrusr.New(params.Loggger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initial manager %v", err)
	}

	jobG := params.JobRegistry.NewGroup(
		params.Scope,
		job.WithLogger(params.Loggger),
		job.WithPprofLabels(pprof.Labels("cells", "controller-runtime")),
	)

	jobG.Add(job.OneShot("manager", func(ctx context.Context, health cell.HealthReporter) error {
		params.Loggger.Info("🚀 Starting controller-runtime manager...")
		fmt.Println("🚀 Starting controller-runtime manager...")

		stopLog := context.AfterFunc(ctx, func() {
			fmt.Println("manager job context canceled:",
				"error=", ctx.Err(),
				"cause=", context.Cause(ctx),
			)
		})
		defer stopLog()

		fmt.Println("calling mgr.Start")
		err := mgr.Start(ctx)
		params.Loggger.WithError(err).WithFields(logrus.Fields{
			"jobContextError": ctx.Err(),
			"jobContextCause": context.Cause(ctx),
		}).Error("Controller-runtime manager stopped")
		fmt.Println("mgr.Start returned:",
			"error=", err,
			"contextError=", ctx.Err(),
			"contextCause=", context.Cause(ctx),
		)
		if err != nil {
			params.Loggger.WithError(err).Error("❌ Manager crashed")
			return err
		}

		params.Loggger.Info("✓ Manager running")
		return nil
	}))

	params.Lifecycle.Append(jobG)

	return mgr, nil
}
