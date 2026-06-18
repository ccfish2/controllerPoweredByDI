package ginkgoext

import (
	"github.com/onsi/ginkgo/v2"
)

var (
	GinkgoWriter = NewWriter(ginkgo.GinkgoWriter)
)
