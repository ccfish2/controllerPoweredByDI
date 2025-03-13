package watches

import (
	"context"
	"fmt"

	"github.com/ccfish2/infra/pkg/bgp/manager"
	"github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/k8s/resource"
	corev1 "k8s.io/api/core/v1"
)

func StartBGPBetaLBAllocator(ctx context.Context, client client.Clientset, services resource.Resource[*corev1.Service]) {
	go func() {
		store, err := services.Store(ctx)
		if err != nil {
			fmt.Println(err)
		}
		m, err := manager.New(ctx, client, store.CacheStore())
		if err != nil {
			fmt.Println(err)
		}
		for ev := range services.Events(ctx) {
			switch ev.Kind {
			case resource.Sync:
				m.Sync()
			case resource.Upsert:
				m.Upsert(nil, ev.Object)
			case resource.Delete:
				m.Delete(ev.Object)
			}
			ev.Done(nil)
		}
	}()
}
