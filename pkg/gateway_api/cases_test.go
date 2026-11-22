package gateway_api

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// go test ./pkg/gateway_api -run TestControllers -args -update

// Data driven specs: one spec per directory below testdata/cases that has an
// input/ directory. To add a test case you only add files:
//
//	testdata/cases/<group>/<case>/input/*.yaml      what to create
//	testdata/cases/<group>/<case>/expected/*.yaml   what must exist afterwards
//	testdata/cases/<group>/<case>/case.yaml         (optional) reconcile order / rounds / ...
//
// Run a single case:   ginkgo --focus "cross-protocol/same-hostname"
// Run a whole group:   ginkgo --focus "cross-protocol/"
// Regenerate expected: go test ./pkg/gateway_api -run TestControllers -args -update
//
// See harness_test.go for what happens in a case.
var _ = Describe("Gateway controller cases", func() {
	cases, err := discoverCases(casesDir)
	if err != nil {
		It("discovers the test cases", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		return
	}
	if len(cases) == 0 {
		It("finds test cases", func() {
			Fail("no test case found below " + casesDir)
		})
		return
	}

	for _, tc := range cases {
		tc := tc
		It(tc.Name, func() {
			runCase(context.Background(), tc)
		})
	}
})
