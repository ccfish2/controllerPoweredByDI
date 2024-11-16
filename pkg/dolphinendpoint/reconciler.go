package dolphinendpoint

import (
	"context"

	"github.com/sirupsen/logrus"

	// dolphin
	v1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/k8s/resource"
)

type reconciler struct {
	logger    logrus.FieldLogger
	context   context.Context
	clientset k8sClient.Clientset

	cepManager operations

	//k8s client accessing k8s extendend API

	depStore resource.Resource[*v1.DolphinEndpoint]
}
