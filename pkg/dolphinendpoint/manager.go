package dolphinendpoint

import (
	v1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
)

type operations interface {
	// External APIs to Insert/Remove CEP in local datastore
	UpdateDEPMapping(dep *v1.DolphinEndpoint, ns string) []DESName
	RemoveDEPMapping(dep *v1.DolphinEndpoint, ns string) DESName

	initializeMappingDEPtoDES(cep *v1.DolphinEndpoint, ns string, ces DESName)
}
