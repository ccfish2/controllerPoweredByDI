package cmd

import (
	"github.com/ccfish2/infra/pkg/ipam/allocator"
)

var allocatorProviders = make(map[string]allocator.AllocatorProvider)
