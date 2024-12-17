package cmd

import (
	"context"
	"sync"

	"k8s.io/client-go/tools/cache"

	// dolphin
	"github.com/ccfish2/infra/pkg/ipam/allocator"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
)

type dolphinNodeSynchronizer struct {
	clientset   k8sclient.Clientset
	nodeManager allocator.NodeEventHandler
	withKVStore bool

	dolphinNodeStore              cache.Store
	k8sDolphinNodesCacheSynced    chan struct{}
	dolphinNodeManagerQueueSynced chan struct{}
}

func newDolphinNodeSynchronizer(cli k8sclient.Clientset, nd allocator.NodeEventHandler, kvstore bool) dolphinNodeSynchronizer {
	return dolphinNodeSynchronizer{}
}

func (d *dolphinNodeSynchronizer) Start(ctx context.Context, wg *sync.WaitGroup) error {
	panic("unimpl")
}
