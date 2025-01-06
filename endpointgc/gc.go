package endpointgc

import (
	"context"
	"time"

	"github.com/ccfish2/infra/pkg/controller"
	"github.com/ccfish2/infra/pkg/hive/cell"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/k8s/resource"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GC struct {
	logger logrus.FieldLogger

	once     bool
	interval time.Duration

	clientset        k8sclient.Clientset
	dolphinendpoints resource.Resource[*dolphinv1.DolphinEndpoint]
	dolphinnodes     resource.Resource[*dolphinv1.DolphinNode]
	pods             resource.Resource[*corev1.Pod]

	mgr     *controller.Manager
	metrics *Metrics
}

// Start implements cell.HookInterface.
func (g *GC) Start(ctx cell.HookContext) error {
	if g.once {
		if !g.checkFoDolphinEndpointCRD(ctx) {
			return nil
		}
		g.interval = 0
		g.logger.Info("run garbage collecotr once")
	} else {
		g.logger.Info("starting garbage collector")
	}
	g.mgr = controller.NewManager()
	g.mgr.UpdateController("gc-controller", controller.ControllerParams{
		Group:       controller.NewGroup("gc-controller-group"),
		RunInterval: g.interval,
		DoFunc:      g.doGC,
	})
	return nil
}

func (g *GC) doGC(ctx context.Context) error {
	CEPStore, err := g.dolphinendpoints.Store(ctx)
	if err != nil {
		return err
	}
	for _, cep := range CEPStore.List() {
		scopedlOg := g.logger.WithFields(logrus.Fields{"": ""})
		if !g.checkIfDepShouldBedeleted(cep, scopedlOg, ctx) {
			continue
		}
		err = g.deleteDEP(cep, scopedlOg, ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

type deleteCheckResult struct {
	shouldBeDeleted bool
	validated       bool
}

func (g *GC) checkIfDepShouldBedeleted(dep *dolphinv1.DolphinEndpoint, scopedlog *logrus.Entry, ctx context.Context) bool {
	if g.once {
		return true
	}
	var podStore resource.Store[*corev1.Pod]
	var dolphinNodeStore resource.Store[*dolphinv1.DolphinNode]
	var err error
	podChecked := false
	podStore, err = g.pods.Store(ctx)
	if err != nil {
		return false
	}
	dolphinNodeStore, err = g.dolphinnodes.Store(ctx)
	if err != nil {
		return false
	}
	for _, owner := range dep.ObjectMeta.OwnerReferences {
		switch owner.Kind {
		case "Pod":
		case "DolphinNode":
		}
	}
	if podChecked {
		_ = podStore
		_ = dolphinNodeStore
		return false
	}
	return true
}

// Stop implements cell.HookInterface.
func (g *GC) Stop(ctx cell.HookContext) error {
	if g.mgr != nil {
		g.mgr.RemoveAllAndWait()
	}
	return nil
}

func (g *GC) deleteDEP(dep *dolphinv1.DolphinEndpoint, scopedLog *logrus.Entry, ctx context.Context) error {
	dolphincli := g.clientset.DolphinV1()
	scopedLog = scopedLog.WithFields(logrus.Fields{"": ""})
	propPolicy := metav1.DeletePropagationBackground
	err := dolphincli.DolphinEndpoints(dep.Namespace).Delete(ctx, dep.Name,
		metav1.DeleteOptions{
			PropagationPolicy: &propPolicy,
			Preconditions: &metav1.Preconditions{
				UID: &dep.UID,
			},
		})
	if err != nil {
		return err
	}
	return nil
}

func (g *GC) checkFoDolphinEndpointCRD(ctx cell.HookContext) bool {
	return true
}

type params struct {
	cell.In

	Logger    logrus.FieldLogger
	Lifecycle cell.Lifecycle

	Clientset        k8sclient.Clientset
	DolphinEndpoints resource.Resource[*dolphinv1.DolphinEndpoint]
	DolphinNodes     resource.Resource[*dolphinv1.DolphinNode]

	SharedCfg SharedConfig
	Metrics   *Metrics
}

func registerGC(p params) {
	// if k8s not enabled, return directly
	if !p.Clientset.IsEnabled() {
		return
	}

	// once check Interval or sharedconfig disablecrd status
	once := p.SharedCfg.Interval == 0 || p.SharedCfg.DisableDolphinEndpointCRD

	// create gc object
	gc := GC{
		once:             once,
		logger:           p.Logger,
		interval:         p.SharedCfg.Interval,
		clientset:        p.Clientset,
		dolphinendpoints: p.DolphinEndpoints,
		metrics:          p.Metrics,
	}
	// dont forget the lifecycle object
	p.Lifecycle.Append(gc)
}
