package cmd

import "github.com/spf13/pflag"

const (
	pprofOperator = "operator-pprof"
	pprofAddress  = "operator-pprof-address"
	pprofPort     = "operator-pprof-port"
)

type operatorPprofConfig struct {
	OperatorPprof        bool
	OperatorPprofAddress string
	OperatorPprofPort    uint16
}

func (cfg operatorPprofConfig) Flags(flags *pflag.FlagSet) {
	flags.Bool(pprofOperator, cfg.OperatorPprof, "Enable serving pprof debugging API")
	flags.String(pprofAddress, cfg.OperatorPprofAddress, "Address that pprof listens on")
	flags.Uint16(pprofPort, cfg.OperatorPprofPort, "Port that pprof listens on")
}
