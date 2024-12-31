package auth

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/auth/identity"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s/resource"
	"github.com/sirupsen/logrus"

	// dolphin
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	"github.com/ccfish2/infra/workerpool"
)

type params struct {
	cell.In

	Logger         logrus.FieldLogger
	LifeCycle      cell.Lifecycle
	IdentityClient identity.IdentityProvider
	Identity       resource.Resource[*dolphinv1.DolphinIdentity]
	Cfg            Config
}

type ItentityWatcher struct {
	logger logrus.FieldLogger

	identityClient identity.IdentityProvider
	identity       resource.Resource[*dolphinv1.DolphinIdentity]
	wg             *workerpool.WorkerPool
	cfg            Config
}

func registerIdentityWatcher(p params) {
	if !p.Cfg.Enabled {
		return
	}
	iw := &ItentityWatcher{
		logger:         p.Logger,
		identityClient: p.IdentityClient,
		identity:       p.Identity,
		wg:             workerpool.New(1),
		cfg:            p.Cfg,
	}
	p.LifeCycle.Append(cell.Hook{
		OnStart: func(hc cell.HookContext) error {
			return iw.wg.Submit(func(ctx context.Context) error {
				return iw.run(ctx)
			})
		},
		OnStop: func(_ cell.HookContext) error {
			return iw.wg.Close()
		},
	})
}

func (iw *ItentityWatcher) run(ctx context.Context) error {
	for e := range iw.identity.Events(ctx) {
		var err error
		switch e.Kind {
		case resource.Upsert:
			err = iw.identityClient.Upsert(ctx, e.Object.GetName())
			iw.logger.WithError(err).Info("Upsert identity")
		case resource.Delete:
			err := iw.identityClient.Delete(ctx, e.Object.GetName())
			iw.logger.WithError(err).Info("Delete identity")
		}
		e.Done(err)
	}
	return nil
}
