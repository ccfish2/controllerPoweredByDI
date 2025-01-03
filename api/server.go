package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"

	"github.com/go-openapi/loads"
	"github.com/go-openapi/runtime"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	// myself and dolphin
	operatorApi "github.com/ccfish2/dolphin/api/v1/operator/server"
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi"
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/metrics"
	"github.com/ccfish2/dolphin/api/v1/operator/server/restapi/operator"
	"github.com/ccfish2/dolphin/pkg/api"
	"github.com/ccfish2/infra/pkg/hive"
	"github.com/ccfish2/infra/pkg/hive/cell"
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

	HealthHandler  operator.GetHealthzHandler
	MetricsHandler metrics.GetMetricsHandler
	apiSpec        *operatorApi.Spec
}

// Start implements cell.HookInterface.
func (s *server) Start(ctx cell.HookContext) error {
	// create spec document for root json

	spec, err := loads.Analyzed(operatorApi.SwaggerJSON, "")
	if err != nil {
		return err
	}
	restapi := restapi.NewDolphinOperatorAPI(spec)
	restapi.Logger = s.logger.Debugf
	restapi.OperatorGetHealthzHandler = s.HealthHandler
	restapi.MetricsGetMetricsHandler = s.MetricsHandler

	api.DisableAPIs(s.apiSpec.DeniedAPIs, restapi.AddMiddlewareFor)
	srv := operatorApi.NewServer(restapi)
	srv.EnabledListeners = []string{"http"}
	srv.ConfigureAPI()
	s.Server = srv

	mux := http.NewServeMux()
	mux.Handle("/", srv.GetHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		resp := s.HealthHandler.Handle(operator.GetHealthzParams{})
		resp.WriteResponse(w, runtime.TextProducer())
	})

	if s.address == "" {
		s.httpSrvs = make([]httpServer, 2)
		s.httpSrvs[0].address = "127.0.0.1:0"
		s.httpSrvs[1].address = "[::1]:0"
	} else {
		s.httpSrvs = make([]httpServer, 1)
		s.httpSrvs[0].address = s.address
	}

	var errs []error
	for i := range s.httpSrvs {
		lc := net.ListenConfig{Control: setsockoptReuseAddrAndPort}
		ln, err := lc.Listen(ctx, "tcp", s.httpSrvs[i].address)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s.httpSrvs[i].listener = ln
		s.httpSrvs[i].server = &http.Server{
			Addr:    s.httpSrvs[i].address,
			Handler: mux}
	}

	if len(s.httpSrvs) == 1 && s.httpSrvs[0].server == nil ||
		len(s.httpSrvs) == 2 && s.httpSrvs[0].server == nil && s.httpSrvs[1].server == nil {
		s.shutdowner.Shutdown()
		return errors.Join(errs...)
	}

	for _, err := range errs {
		s.logger.Warnf("failed to start server: %v", err)
	}

	for _, srv := range s.httpSrvs {
		if srv.server == nil {
			continue
		}
		go func(srv httpServer) {
			if err := srv.server.Serve(srv.listener); !errors.Is(err, http.ErrServerClosed) {
				s.logger.Warnf("failed to serve: %v", err)
			}
		}(srv)
	}
	return nil
}

// Stop implements cell.HookInterface.
func (s *server) Stop(ctx cell.HookContext) error {
	for _, srv := range s.httpSrvs {
		if srv.server == nil {
			continue
		}
		if err := srv.server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

type httpServer struct {
	address  string
	listener net.Listener
	server   *http.Server
}

func (s *server) Ports() []int {
	var ports []int
	for _, srv := range s.httpSrvs {
		if srv.server == nil {
			continue
		}
		ports = append(ports, srv.listener.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

func newServer(p params) (Server, error) {
	srv := &server{
		logger:         p.Logger,
		shutdowner:     p.Shutdown,
		HealthHandler:  p.HealthHandler,
		MetricsHandler: p.MetricsHandler,
		apiSpec:        p.OperatorAPISpec,
		address:        p.cfg.OPeratorAPIServeAddr,
	}
	p.LC.Append(srv)

	return srv, nil
}

func setsockoptReuseAddrAndPort(network, address string, c syscall.RawConn) error {
	var soerr error
	if err := c.Control(func(fd uintptr) {
		s := int(fd)
		if err := unix.SetsockoptInt(s, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			soerr = fmt.Errorf("failed to set SO_REUSEADDR: %w", err)
			return
		}
		soerr = unix.SetsockoptInt(s, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	}); err != nil {
		return err
	}
	return soerr
}
