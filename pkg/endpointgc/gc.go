package endpointgc

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
)

type GC struct {
	// logger

	// once

	// interval periodically check time duration

	// clientset interface to access k8s client
	// dolphin endpoints
	// to make up endpoints, node and pods are needed
	// mgr controller for endpoint
}
type params struct {
	cell.In

	// logger
	// Lifecycle

	// clientset
	// endpoints

	// sharedCfg
}

func registerGC(p params) {
	// if k8s not enabled, return directly

	// once check Interval or sharedconfig disablecrd status

	// create gc object
	// dont forget the lifecycle object
}
