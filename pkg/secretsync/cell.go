package secretsync

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"secret-sync",
	"Syncs TLS secrets into a dedicated secrets namespace",
)

type SecretSyncRegistrationOut struct {
	cell.Out

	SecretSyncRegistration *SecretSyncRegistration `group:"secretSyncRegistrations"`
}
