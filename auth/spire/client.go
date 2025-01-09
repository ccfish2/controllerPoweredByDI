package spire

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	"google.golang.org/grpc"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/auth/identity"
	// dolphin
	"github.com/ccfish2/infra/pkg/hive/cell"
	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
)

const ()

var Cell = cell.Module(
	"spire-client",
	"Spire Server API Client",
	cell.Config(ClientConfig{}),
	cell.Provide(NewClient),
)

type ClientConfig struct {
	MutualAuthEnabled    bool          `mapstructure:"mesh-auth-mutual-enabled,omitempty"`
	SpireAgentSocketPath string        `mapstructure:"mesh-auth-spire-agent-socket,omitempty"`
	SpireServerAddress   string        `mapstructure:"mesh-auth-spire-server-address,omitempty"`
	SpireServerTimeout   time.Duration `mapstructure:"mesh-auth-spire-server-timeout"`
	SpiffeTrustDomain    string        `mapstructure:"mesh-auth-spiffe-trust-domain"`
}

func (cfg ClientConfig) Flags(flags *pflag.FlagSet) {
	flags.BoolVar(&cfg.MutualAuthEnabled, "mesh-auth-mutual-enabled", true, "")
	flags.StringVar(&cfg.SpireAgentSocketPath, "mesh-auth-spire-agent-socket", "/run/spire/sockets/agent/agent.sock", "")
	flags.StringVar(&cfg.SpireServerAddress, "mesh-auth-spire-server-address", "spire-server.spire.io:8081", "")
	flags.DurationVar(&cfg.SpireServerTimeout, "mesh-auth-spire-server-timeout", 10*time.Second, "")
	flags.StringVar(&cfg.SpiffeTrustDomain, "mesh-auth-spiffe-trust-domain", "spiffe.dolphin", "")

}

type params struct {
	cell.In

	K8sClient k8sclient.Clientset
}

type Client struct {
	cfg    ClientConfig
	logger logrus.FieldLogger
	entry  entryv1.EntryClient

	k8sClient k8sclient.Clientset
}

func NewClient(p params, lc cell.Lifecycle, cfg ClientConfig, log logrus.FieldLogger) identity.Provider {
	if !p.K8sClient.IsEnabled() {
		return nil
	}
	c := Client{}

	return c
}

func (c Client) List(ctx context.Context) ([]string, error) {

	panic("impl me")
}

func (c Client) onStart(_ cell.HookContext) error {
	panic("implement me")
}

func (c *Client) connect(ctx context.Context) (*grpc.ClientConn, error) {
	panic("implement me")
}

func (c Client) Upsert(ctx context.Context, id string) error {
	panic("implement me")
}

func (c Client) Delete(ctx context.Context, id string) error {
	panic("implement me")

}

func (c *Client) listEntries(ctx context.Context, id string) (*entryv1.ListEntriesResponse, error) {
	panic("implement me")
}

func resolvedK8sService(ctx context.Context, client k8sclient.Clientset, address string) (*string, error) {
	panic("implement me")
}
