package secretsync

import "github.com/ccfish2/infra/pkg/hive/cell"

var Cell = cell.Module(
	"secret-sync",
	"Syncs TLS secrets into a dedicated secrets namespace",
)
