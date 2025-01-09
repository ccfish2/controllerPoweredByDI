package auth

import (
	"github.com/ccfish2/controllerPoweredByDI/auth/spire"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/spf13/pflag"
)

const (
	mutualAuthEnabled = "mesh-mutual-auth"
)

var Cell = cell.Module(
	"auth",
	"Mesh Mutual Auth",
	spire.Cell,
	cell.Config(Config{}),
	cell.Invoke(registerIdentityWatcher),
)

type Config struct {
	Enabled bool `mapstructure:"mutual_auth_enabled,omitempty"`
}

func (def Config) Flags(cf *pflag.FlagSet) {
	cf.Bool(mutualAuthEnabled, def.Enabled, "")
}
