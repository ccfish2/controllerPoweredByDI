package identitygc

import (
	"fmt"
	"time"

	"github.com/ccfish2/infra/pkg/controller"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/workerpool"
	"github.com/sirupsen/logrus"

	//mysef
	authIdentity "github.com/ccfish2/controllerPoweredByDI/auth/identity"

	// dolphin
	"github.com/ccfish2/infra/pkg/allocator"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
	dolphinVersion1 "github.com/ccfish2/infra/pkg/k8s/client/clientset/versioned/typed/dolphin.io/v1"
	"github.com/ccfish2/infra/pkg/k8s/resource"
	"github.com/ccfish2/infra/pkg/option"
	"github.com/ccfish2/infra/pkg/rate"

	cmtypes "github.com/ccfish2/infra/pkg/clustermesh/types"
)

type params struct {
	cell.In

	Logger    logrus.FieldLogger
	Lifecycle cell.Lifecycle

	Clientset          k8sclient.Clientset
	Identity           resource.Resource[*dolphinv1.DolphinIdentity]
	Endpoints          resource.Resource[*dolphinv1.DolphinEndpoint]
	AuthIdentityClient authIdentity.Provider

	Cfg          Config
	SharedConfig SharedConfig
	ClusterInfo  cmtypes.ClusterInfo

	Metrics *Metrics
}

type GC struct {
	logger    logrus.FieldLogger
	lifecycle cell.Lifecycle

	clientset          dolphinVersion1.DolphinIdentityInterface
	identity           resource.Resource[*dolphinv1.DolphinIdentity]
	endpoints          resource.Resource[*dolphinv1.DolphinEndpoint]
	authIdentityClient authIdentity.Provider

	clusterInfo    cmtypes.ClusterInfo
	allocationMode string

	gcInterval       time.Duration
	heartbeatTimeout time.Duration
	gcRateInterval   time.Duration
	gcRateLimit      int64

	wp             *workerpool.WorkerPool
	heartbeatStore *heartbeatStore
	mgr            controller.Manager

	rateLimiter *rate.Limiter

	allocationConfig identityAllocationConfig
	allocator        *allocator.Allocator

	failedRuns     int64
	successfulRuns int64
	metrics        *Metrics
}

func registerGC(p params) {
	if !p.Clientset.IsEnabled() {
		return
	}

	gc := GC{
		logger:             p.Logger,
		clientset:          p.Clientset.DolphinV1().DolphinIdentities(),
		identity:           p.Identity,
		endpoints:          p.Endpoints,
		authIdentityClient: p.AuthIdentityClient,

		clusterInfo:      p.ClusterInfo,
		allocationMode:   p.SharedConfig.IdentityAllocationMode,
		gcInterval:       p.Cfg.Interval,
		heartbeatTimeout: p.Cfg.HeartbeatTimeout,
		gcRateInterval:   p.Cfg.RateInterval,
		gcRateLimit:      p.Cfg.RateLimit,
		heartbeatStore:   newheartbeatStore(p.Cfg.HeartbeatTimeout),
		rateLimiter: rate.NewLimiter(
			p.Cfg.RateInterval,
			p.Cfg.RateLimit,
		),
		allocationConfig: identityAllocationConfig{k8snamespce: p.SharedConfig.K8sNamespace},
		metrics:          p.Metrics,
	}
	p.Lifecycle.Append(cell.Hook{
		OnStart: func(ctx cell.HookContext) error {
			gc.wp = workerpool.New(1)
			switch gc.allocationMode {
			case option.IdentityAllocationModeCRD:
				return gc.startCRDModeGC(ctx)
			case option.IdentityAllocationModeKVstore:
				return gc.startKVStoreModeGC(ctx)
			default:
				return fmt.Errorf("unknown identity allocation mode", gc.allocationMode)
			}
		},
		OnStop: func(hc cell.HookContext) error {
			if gc.allocationMode == option.IdentityAllocationModeCRD {
				gc.mgr.RemoveAllAndWait()
			}
			gc.rateLimiter.Stop()
			gc.wp.Close()

			return nil
		},
	})
}

type identityAllocationConfig struct {
	k8snamespce string
}

func (g *GC) startCRDModeGC(ctx cell.HookContext) error {
	return nil
	//panic("impl start CRD mode GC")
}

func (g *GC) startKVStoreModeGC(ctx cell.HookContext) error {
	return nil
	//panic("impl start KVstore mode GC")
}
