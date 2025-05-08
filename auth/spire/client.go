package spire

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	workloadapi "github.com/spiffe/go-spiffe/v2/workloadapi"
	entryv1 "github.com/spiffe/spire-api-sdk/proto/spire/api/server/entry/v1"
	"github.com/spiffe/spire-api-sdk/proto/spire/api/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/auth/identity"

	// dolphin
	"github.com/ccfish2/infra/pkg/backoff"
	"github.com/ccfish2/infra/pkg/hive/cell"

	k8sclient "github.com/ccfish2/infra/pkg/k8s/client"
	"github.com/ccfish2/infra/pkg/logging/logfields"
)

const (
	defaultParentID = "/dolphin-operator"
	pathPrfix       = "identity"
)

var defaultSelectors = []*types.Selector{
	{
		Type:  "dolphin",
		Value: "mutual-auth",
	},
}

var Cell = cell.Module(
	"spire-client",
	"Spire Server API Client",
	//cell.Config(ClientConfig{}),
	//cell.Provide(NewClient),
)

type ClientConfig struct {
	MutualAuthEnabled            bool          `mapstructure:"mesh-auth-mutual-enabled,omitempty"`
	SpireAgentSocketPath         string        `mapstructure:"mesh-auth-spire-agent-socket,omitempty"`
	SpireClientEnabled           bool          `mapstructure:"mesh-auth-spire-client-enabled,omitempty"`
	SpireServerAddress           string        `mapstructure:"mesh-auth-spire-server-address,omitempty"`
	SpireServerConnectionTimeout time.Duration `mapstructure:"mesh-auth-spire-server-timeout"`
	SpiffeTrustDomain            string        `mapstructure:"mesh-auth-spiffe-trust-domain"`
}

func (cfg ClientConfig) Flags(flags *pflag.FlagSet) {
	flags.BoolVar(&cfg.SpireClientEnabled, "mesh-auth-spire-client-enabled", false, "")
	flags.BoolVar(&cfg.MutualAuthEnabled, "mesh-auth-mutual-enabled", false, "")
	flags.StringVar(&cfg.SpireAgentSocketPath, "mesh-auth-spire-agent-socket", "/run/spire/sockets/agent/agent.sock", "")
	flags.StringVar(&cfg.SpireServerAddress, "mesh-auth-spire-server-address", "spire-server.spire.io:8081", "")
	flags.DurationVar(&cfg.SpireServerConnectionTimeout, "mesh-auth-spire-server-timeout", 10*time.Second, "")
	flags.StringVar(&cfg.SpiffeTrustDomain, "mesh-auth-spiffe-trust-domain", "spiffe.dolphin", "")

}

type params struct {
	cell.In

	K8sClient k8sclient.Clientset
	CliCfg    ClientConfig
}

type Client struct {
	cfg    ClientConfig
	logger logrus.FieldLogger
	entry  entryv1.EntryClient

	k8sClient k8sclient.Clientset
}

func NewClient(p params, lc cell.Lifecycle, cfg ClientConfig, log logrus.FieldLogger) identity.Provider {
	if !p.CliCfg.SpireClientEnabled {
		return nil
	}
	if !p.K8sClient.IsEnabled() {
		return nil
	}
	c := Client{
		k8sClient: p.K8sClient,
		cfg:       cfg,
		logger:    log.WithField(logfields.LogSubsys, "spire-client"),
	}

	lc.Append(
		cell.Hook{
			OnStart: c.onStart,
			OnStop: func(_ cell.HookContext) error {
				return nil
			}})

	return c
}

func (c Client) List(ctx context.Context) ([]string, error) {
	entires, err := c.entry.ListEntries(ctx, &entryv1.ListEntriesRequest{
		Filter: &entryv1.ListEntriesRequest_Filter{
			ByParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path:        defaultParentID,
			},
			BySelectors: &types.SelectorMatch{
				Selectors: defaultSelectors,
				Match:     types.SelectorMatch_MATCH_EXACT,
			},
		}})
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
	go func() {
		c.logger.Info("Initializing Spire Client")
		attempts := 0
		backOffTime := backoff.Exponential{Min: 100 * time.Millisecond, Max: 10 * time.Second}
		for {
			attempts++
			conn, err := c.connect(context.Background())
			if err == nil {
				c.entry = entryv1.NewEntryClient(conn)
				break
			}
			c.logger.WithError(err).WithField("attempts", attempts).Error("Failed to connect to Spire Server")
			time.Sleep(backOffTime.Duration(attempts))
		}
		c.logger.Info("Spire Client initialized successfully")
	}()
	return nil
}

func (c *Client) connect(ctx context.Context) (*grpc.ClientConn, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, c.cfg.SpireServerConnectionTimeout)
	defer cancel()

	resolvedTarget, err := resolvedK8sService(ctx, c.k8sClient, c.cfg.SpireServerAddress)
	if err != nil {
		c.logger.WithError(err).
			WithField("url", c.cfg.SpireServerAddress).
			Warning("Unable to resolve SPIRE server address, using original value")
		resolvedTarget = &c.cfg.SpireServerAddress
	}

	source, err := workloadapi.NewX509Source(
		timeoutCtx,
		workloadapi.WithClientOptions(
			workloadapi.WithAddr(fmt.Sprintf("unix://%s", c.cfg.SpireAgentSocketPath)),
			workloadapi.WithLogger(newSpiffeLogWrapper(c.logger)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed init x509 resource %w", err)
	}

	trustDomain, err := spiffeid.TrustDomainFromString(c.cfg.SpiffeTrustDomain)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %w", err)
	}
	tlsConfig := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeMemberOf(trustDomain))

	c.logger.WithFields(logrus.Fields{
		logfields.Address: c.cfg.SpireServerAddress,
		logfields.IPAddr:  resolvedTarget,
	}).Info("Trying to connect to SPIRE server")
	conn, err := grpc.Dial(*resolvedTarget, grpc.WithTransportCredentials(
		credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to create connection to SPIRE server: %w", err)
	}
	c.logger.WithFields(logrus.Fields{
		logfields.Address: c.cfg.SpireServerAddress,
		logfields.IPAddr:  resolvedTarget,
	}).Info("Connected to SPIRE server")
	return conn, nil
}

func (c Client) Upsert(ctx context.Context, id string) error {
	if c.entry == nil {
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
				Path:        toPath(id),
			},
			ParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path:        defaultParentID,
			},
			Selectors: defaultSelectors,
		},
	}
	if entries == nil || len(entries.Entries) == 0 {
		_, err := c.entry.BatchCreateEntry(ctx, &entryv1.BatchCreateEntryRequest{
			Entries: desired,
		})
		return err
	}

	_, err = c.entry.BatchUpdateEntry(ctx, &entryv1.BatchUpdateEntryRequest{
		Entries: desired,
	})
	return err
}

func (c Client) Delete(ctx context.Context, id string) error {
	if c.entry == nil {
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
		ids = append(ids, e.Id)
	}
	_, err = c.entry.BatchDeleteEntry(ctx, &entryv1.BatchDeleteEntryRequest{
		Ids: ids,
	})
	return err
}

func (c *Client) listEntries(ctx context.Context, id string) (*entryv1.ListEntriesResponse, error) {
	return c.entry.ListEntries(ctx, &entryv1.ListEntriesRequest{
		Filter: &entryv1.ListEntriesRequest_Filter{
			BySpiffeId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path:        toPath(id),
			},
			ByParentId: &types.SPIFFEID{
				TrustDomain: c.cfg.SpiffeTrustDomain,
				Path:        defaultParentID,
			},
			BySelectors: &types.SelectorMatch{
				Selectors: defaultSelectors,
				Match:     types.SelectorMatch_MATCH_EXACT,
			},
		},
	})
}

func resolvedK8sService(ctx context.Context, client k8sclient.Clientset, address string) (*string, error) {
	names := strings.Split(address, ":")
	if len(names) < 3 || !strings.HasPrefix(names[2], "svcc") {
		return &address, nil
	}

	svc, err := client.CoreV1().Services(names[1]).Get(ctx, names[0], metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	res := net.JoinHostPort(svc.Spec.ClusterIP, port)
	return &res, nil
}

func toPath(id string) string {
	return fmt.Sprintf("%v/%v", pathPrfix, id)
}
