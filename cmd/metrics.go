package cmd

import (
	"github.com/spf13/cobra"
)

var MetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Access metric status of the operator",
}
