package api

import (
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/operator"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/go-openapi/runtime/middleware"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/discovery"
)

type kvstoreEnabledFunc func() bool
type isOperatorLeadingFunc func() bool

func HealthHandlerCell(
	kvstoreEnabled kvstoreEnabledFunc,
	isOperatorLeading isOperatorLeadingFunc) cell.Cell {
	return cell.Module(
		"health-handler",
		"Operator health HTTP Handler",
		cell.Provide(func(clientset client.Clientset, logger logrus.FieldLogger) operator.GetHealthzHandler {
			if !clientset.IsEnabled() {
				return &healthhandler{
					enabled: false,
				}
			}
			return &healthhandler{
				enabled:           true,
				kvstoreEnabled:    kvstoreEnabled,
				isOperatorLeading: isOperatorLeading,
				discovery:         clientset.Discovery(),
				log:               logger,
			}
		}),
	)
}

type healthhandler struct {
	enabled           bool
	kvstoreEnabled    kvstoreEnabledFunc
	isOperatorLeading isOperatorLeadingFunc
	discovery         discovery.DiscoveryInterface
	log               logrus.FieldLogger
}

func (h *healthhandler) Handle(params operator.GetHealthzParams) middleware.Responder {
	if !h.enabled {
		return operator.NewGetHealthzNotImplemented()
	}
	if err := h.checkStatus(); err != nil {
		h.log.WithError(err).Error("Health check status")
		return operator.NewGetHealthzInternalServerError().WithPayload(err.Error())
	}
	return operator.NewGetHealthzOK().WithPayload("ok")
}

func (h *healthhandler) checkStatus() error {
	_, err := h.discovery.ServerVersion()
	return err
}
