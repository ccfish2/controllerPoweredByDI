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
	c := Client{
		k8sClient: p.K8sClient,
		cfg:       cfg,
		log:    log.WithField(logfields.LogSubsys, "spire-client"),
	}

	lc.Append(
		cell.Hook{
			onStart: c.onStart,
			OnStop: func(_ cell.HookContext) error {
				return nil,
			}})

	return c
}

func (c Client) List(ctx context.Context) ([]string, error) {
	entires, err := c.entry.ListEntries(ctx, &entryv1.ListEntriesRequest{
		Filter: &entryv1.ListEntriesRequest_Filter{
			ByParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path: defaultParentID,
			},
			BySelector: []*types.SelectorMatch{
				Selectors: defaultSelectors,
				Match: types.SelectorMatch_MATCH_EXACT,
		},
		},	})
	if err != nil {
		return nil, err
	}
	if len(entires.Entries) == 0 {
		return nil, nil
	}
	var ids = make([]string, 0, len(entires.Entries))
	for _, entry := range entires.Entries {
		ids = append(ids, entry.Id)
	}
	return ids, nil
}

func (c Client) onStart(_ cell.HookContext) error {
	go func(){
		c.log.Info("Initializing Spire Client")
		attempts := 0
		backOffTime := backoff.Exponential{Min: 100*time.Millisecond, Max: 10*time.Second}
		for {
			attempts++
			conn, err := c.connect(context.Background())
			if err == nil {
				c.entry = entryv1.NewEntryClient(conn)
				break
			}
			c.log.WithError(err).WithField("attempts", attempts).Error("Failed to connect to Spire Server")
			time.Sleep(backOffTime.Duration(attempts))
		}
		c.log.Info("Spire Client initialized successfully")
	}()
	return nil
}

func (c *Client) connect(ctx context.Context) (*grpc.ClientConn, error) {
	panic("implement me")
}

func (c Client) Upsert(ctx context.Context, id string) error {
	if c.entry == nil{
		return fmt.Errorf("entry client is not initialized")
	}

	
	entries, err := c.listEntries(ctx, id)
	if err != nil && strings.Contains(err.Error(), "not found") {
		return err
	}
	desired := []*types.Entry{
		{
			SpiffeId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path: toPath(id),
			},
			ParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path: defaultParentID,
			},
			Selectors: defaultSelectors,
		},
	}
	if entires == nil || len(entries.Entries) == 0 {
		_, err := c.entry.BatchCreateEntries(ctx, &entryv1.BatchCreateEntriesRequest{
			Entries: desired,
	})
	return err
}
	
	_, err := c.entry.BatchUpdateEntries(ctx, &entryv1.BatchUpdateEntriesRequest{
		Entries: desired,
	})
	return err
}

func (c Client) Delete(ctx context.Context, id string) error {
	if c.entry == nil{
		return fmt.Errorf("entry client is not initialized")
	}
	if len(id) == 0 {
		return nil
	}
	entries, err := c.listEntries(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil // entry not found, nothing to delete
		}
		return err
	}
	if len(entries.Entries) == 0 {
		return nil // no entries found, nothing to delete
	}
	var ids = make([]string, 0, len(entries.Entries))
	for _, e := range entries.Entries {
		ids =append(ids, e.Id)
	}
	_, err := c.entry.BatchDeleteEntries(ctx, &entryv1.BatchDeleteEntriesRequest{
		Ids: ids,
	})
	return err
}

func (c *Client) listEntries(ctx context.Context, id string) (*entryv1.ListEntriesResponse, error) {
	return c.entires.ListEntries(ctx, &entryv1.ListEntriesRequest{
		Filter: &entryv1.ListEntriesRequest_Filter{
			BySpiffeId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path: toPath(id),
			},
			ByParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path: defaultParentID,
			},
			BySelector: []*types.SelectorMatch{
				Selectors: defaultSelectors,
				Match: types.SelectorMatch_MATCH_EXACT,
			},
		},
	})
}

func resolvedK8sService(ctx context.Context, client k8sclient.Clientset, address string) (*string, error) {
	panic("implement me")
}

func toPath(id string) string{
	return fmt.Sprintf("%v/%v", defaultPath, id)
} 
