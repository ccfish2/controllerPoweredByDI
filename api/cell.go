package api

import (
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

// bump one server receiving requests from operator
var ServerCell = cell.Module(
	"dolphin-operator-api",
	"Dolphin Operator API Server",
	cell.Config(Config{}),
	cell.Provide(newServer),
	cell.Invoke(func(Server) {}),
)

const (
	// OperatorAPIServeAddr is the "ip:port" serve operator api request
	// ":port" bind all interfaces
	// use empty string bind both IPv4 and IPv6 default interface
	OperatorAPIServeAddr = "operator-api-serve-addr"
)

const (
	// api requests from the operator
	operatorServeAddrDefault = "localhost:9233"
)

type Config struct {
	OperatorAPIServeAddr string
}

func (def Config) Flags(flags *pflag.FlagSet) {
	flags.String(OperatorAPIServeAddr, operatorServeAddrDefault, "The address to serve the operator API request")
}
