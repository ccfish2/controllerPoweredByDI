package api

import (
	"errors"

	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/operator"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/kvstore"
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
		"Operator-HTTP-Health-Handler",
		cell.Provide(func(clientset client.Clientset, log logrus.FieldLogger) operator.GetHealthzHandler {
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
				log:               log,
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
		return operator.NewGetHealthzInternalServerError().WithPayload(err.Error())
	}
	return operator.NewGetHealthzOK().WithPayload("ok")
}

func (h *healthhandler) checkStatus() error {
	if h.kvstoreEnabled() && h.isOperatorLeading() {
		client := kvstore.Client()
		if client == nil {
			return errors.New("kvstore client not configured")
		}
		if err := client.Status(); err != nil {
			return err
		}
	}
	_, err := h.discovery.ServerVersion()
	return err
}
