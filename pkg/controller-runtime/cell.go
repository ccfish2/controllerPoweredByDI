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

	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
)

var Cell = cell.Module(
	"controller-runtime",
	"Manages the controller-runtime integration and its components",
	cell.Invoke(NewScheme),
	cell.Invoke(NewManager),
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
		return mgr.Start(ctx)
	}))

	params.Lifecycle.Append(jobG)

	return mgr, nil
}
