package k8s

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	// dolpin
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/k8s/resource"
	"github.com/ccfish2/infra/pkg/k8s/utils"

	// DOLPHIN
	dolphin_api_v1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

func DolphinEndpointResource(lc cell.Lifecycle, cs client.Clientset, opts ...func(*metav1.ListOptions)) (resource.Resource[*dolphin_api_v1.DolphinEndpoint], error) {
	if !cs.IsEnabled() {
		return nil, nil
	}
	lw := utils.ListerWatcherWithModifiers(
		utils.ListerWatcherFromTyped[*dolphin_api_v1.DolphinEndpointList](cs.DolphinV1().DolphinEndpoints("")),
		opts...,
	)

	indexers := cache.Indexers{
		cache.NamespaceIndex:         cache.MetaNamespaceIndexFunc,
		DolphinEndpointIndexIdentity: identityIndexFunc,
	}
	return resource.New[*dolphin_api_v1.DolphinEndpoint](
		lc, lw, resource.WithMetric("DolphinEndpoint"), resource.WithIndexers(indexers)), nil

}

func identityIndexFunc(obj interface{}) ([]string, error) {
	switch t := obj.(type) {
	case *dolphin_api_v1.DolphinEndpoint:
		if t.Status.Checksum != 0 {
			tv := strconv.FormatInt(t.Status.Checksum, 10)
			return []string{tv}, nil
		}
	}
	return nil, fmt.Errorf("failed retrieving identity from the object")
}
