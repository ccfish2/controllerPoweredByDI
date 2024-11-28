package cmd

import (
	"testing"

	"github.com/ccfish2/infra/pkg/hive"
	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
)

func TestOperatorHive(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreCurrent(),
	)

	err := hive.New(Operator).Populate()
	assert.NoError(t, err, "Populate()")
}
