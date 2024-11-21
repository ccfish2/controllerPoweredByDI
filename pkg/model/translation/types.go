package translation

import (
	"github.com/ccfish2/controller-powered-by-DI/pkg/model"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	v1 "k8s.io/api/core/v1"
)

type Translator interface {
	Translate(*model.Model) (*dolphinv1.DolphinEnvoyConfig, *v1.Service, *v1.Endpoints, error)
}
