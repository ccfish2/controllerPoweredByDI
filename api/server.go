package api

import (
	"net"
	"net/http"

	operatorApi "github.com/ccfish2/dolphin/api/v1/operator/server"
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/metrics"
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/operator"
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
	"github.com/sirupsen/logrus"
)

type Server interface {
	Ports() []int
}

type params struct {
	cell.In

	cfg Config

	HealthHandler   operator.GetHealthzHandler
	MetricsHandler  metrics.GetMetricsHandler
	OperatorAPISpec *operatorApi.Spec

	Logger   logrus.FieldLogger
	LC       cell.Lifecycle
	Shutdown hive.Shutdowner
}

type server struct {
	*operatorApi.Server

	logger     logrus.FieldLogger
	shutdowner hive.Shutdowner

	address  string
	httpSrvs []httpServer

	HealthHandler   operator.GetHealthzHandler
	MetricsHandler  metrics.GetMetricsHandler
	OperatorAPISpec *operatorApi.Spec
}

// Start implements cell.HookInterface.
func (s *server) Start(cell.HookContext) error {
	panic("unimplemented")
}

// Stop implements cell.HookInterface.
func (s *server) Stop(cell.HookContext) error {
	panic("unimplemented")
}

type httpServer struct {
	address  string
	listener net.Listener
	server   *http.Server
}

func (s *server) Ports() []int {
	panic("unimplemented")
}
func newServer(p params) (Server, error) {
	srv := &server{
		logger:          p.Logger,
		shutdowner:      p.Shutdown,
		HealthHandler:   p.HealthHandler,
		MetricsHandler:  p.MetricsHandler,
		OperatorAPISpec: p.OperatorAPISpec,
		address:         p.cfg.OPeratorAPIServeAddr,
	}
	p.LC.Append(srv)

	return srv, nil
}
