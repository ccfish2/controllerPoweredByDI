package api

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

// bump one server receiving requests from operator
var ServerCell = cell.Module(
	"dolphin-operator-api",
	"Dolphin Operator API Server",
	cell.Config(&Config{}),
	cell.Provide(newServer),
	cell.Invoke(func(Server) {}),
)

const (
	OperatorAPIServeAddr = "operator-api-serve-addr"
)

const (
	operatorServeAddrDefault = "localhost:9234"
)

type Config struct {
	OPeratorAPIServeAddr string
}

func (def Config) Flags(flags *pflag.FlagSet) {
	flags.String(OperatorAPIServeAddr, def.OPeratorAPIServeAddr, "The address to serve the operator API")
}
