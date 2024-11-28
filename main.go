package main

import (
	"github.com/ccfish2/controllerPoweredByDI/cmd"
	"github.com/ccfish2/infra/pkg/hive"
)

func main() {
	operatorHive := hive.New(cmd.Operator)
	cmd.Execute(cmd.NewOperatorCmd(operatorHive))
}
