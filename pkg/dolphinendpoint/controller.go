package dolphinendpoint

import (
	"context"

	"github.com/sirupsen/logrus"

	// myself

	// dolphin
	"github.com/ccfish2/infra/pkg/hive/cell"
	v1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"
	k8sClient "github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/k8s/resource"
)

type params struct {
	cell.In

	Logger    logrus.FieldLogger
	Lifecycle cell.Lifecycle

	Clientset       k8sClient.Clientset
	DolphinEndpoint resource.Resource[*v1.DolphinEndpoint]

	Cfg       Config
	SharedCfg SharedConfig

	// todo: add metrics back into the controller
}

type Controller struct {
	logger        logrus.FieldLogger
	context       context.Context
	contextCancel context.CancelFunc

	//k8s client accessing k8s extendend API
	clientset       k8sClient.Clientset
	dolphinendpoint resource.Resource[*v1.DolphinEndpoint]

	// reconciler is an util used to reconcile dolphinendpointslice changes
	reconciler *reconciler
}
