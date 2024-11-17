package option

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

var IngressLBAnnotationsDefault = []string{"service.beta.kubernetes.io", "service.kubernetes.io", "cloud.google.com"}

const (
	EndpointGCIntervalDefault      = 5 * time.Minute
	DESMaxCEPsInDESDefault         = 100
	DESSlicingModeDefault          = "desSliceModeIdentity"
	DESWriteQPSLimitDefault        = 10
	DESWriteQPSLimitMax            = 50
	DESWriteQPSBurstDefault        = 20
	DESWriteQPSBurstMax            = 100
	DNPStatusCleanupQPSDefault     = 10
	DNPStatusCleanupBurstDefault   = 20
	PprofAddressOperator           = "localhost"
	PprofPortOperator              = 6061
	DefaultProxyIdleTimeoutSeconds = 60
)

const (
	EnableGatewayAPI = "enable-gateway-api"

	BGPAnnounceLBIP = "bgp-announce-lb-ip"
	BGPConfigPath   = "bgp-config-path"

	SkipDNPStatusStartupClean = "skip-dnp-status-startup-clean"
	DNPStatusCleanupQPS       = "dnp-status-cleanup-qps"
	DNPStatusCleanupBurst     = "dnp-status-cleanup-burst"

	EnableMetrics = "enable-metrics"

	EndpointGCInterval          = "dolphin-endpoint-gc-interval"
	NodesGCInterval             = "nodes-gc-interval"
	SyncK8sServides             = "synchronize-k8s-servides"
	SyncK8sNodes                = "synchronize-k8s-nodes"
	UnmanagedPodWatcherInterval = "unmanaged-pod-watcher-interval"

	// IPAM options

	IPAMAPIBurst    = "limit-ipam-api-burst"
	IPAMAPIQPSLimit = "limit-ipam-api-qps"

	IPAMSubnetsIDs  = "subnet-ids-filter"
	IPAMSubnetsTags = "subnet-tags-filter"

	IPAMInstanceTags                = "instance-tags-filter"
	IPAMAutoCreateDolphinPodIPPools = "auto-create-dolphin-pod-ip-pools"
	ClusterPoolIPv4CIDR             = "cluster-pool-ipv4-cidr"
	ClusterPoolIPv6CIDR             = "cluster-pool-ipv6-cidr"
	NodeCIDRMaskSizeIPv4            = "cluster-pool-ipv4-mask-size"
	NodeCIDRMaskSizeIPv6            = "cluster-pool-ipv6-mask-size"

	// AWS options
	AWSInstanceLimitMapping      = "aws-instance-limit-mapping"
	AWSReleaseExdessIPs          = "aws-release-exdess-ips"
	ExdessIPReleaseDelay         = "exdess-ip-release-delay"
	AWSEnablePrefixDelegation    = "aws-enable-prefix-delegation"
	ENITags                      = "eni-tags"
	ENIGarbageCollectionTags     = "eni-gc-tags"
	ENIGarbageCollectionInterval = "eni-gc-interval"
	ParallelAllocWorkers         = "parallel-alloc-workers"
	UpdateEC2AdapterLimitViaAPI  = "update-ec2-adapter-limit-via-api"
	EC2APIEndpoint               = "ec2-api-endpoint"
	AWSUsePrimaryAddress         = "aws-use-primary-address"

	LeaderElectionLeaseDuration = "leader-election-lease-duration"
	LeaderElectionRenewDeadline = "leader-election-renew-deadline"
	LeaderElectionRetryPeriod   = "leader-election-retry-period"

	LoadBalancerL7 = "loadbalancer-l7"

	ProxyIdleTimeoutSeconds = "proxy-idle-timeout-seconds"

	DolphinK8sNamespace             = "dolphin-pod-namespace"
	DolphinPodLabels                = "dolphin-pod-labels"
	RemoveDolphinNodeTaints         = "remove-dolphin-node-taints"
	SetDolphinNodeTaints            = "set-dolphin-node-taints"
	SetDolphinIsUpCondition         = "set-dolphin-is-up-condition"
	IngressDefaultXffNumTrustedHops = "ingress-default-xff-num-trusted-hops"
	PodRestartSelector              = "pod-restart-selector"
)

// OperatorConfig is the configuration used by the operator.
type OperatorConfig struct {
	NodesGCInterval           time.Duration
	SkipDNPStatusStartupClean bool
	DNPStatusCleanupQPS       float64
	DNPStatusCleanupBurst     int
	EnableMetrics             bool
	EndpointGCInterval        time.Duration

	SyncK8sServides             bool
	SyncK8sNodes                bool
	UnmanagedPodWatcherInterval int

	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration

	BGPAnnounceLBIP bool
	BGPConfigPath   string

	// IPAM options
	IPAMAPIBurst     int
	IPAMAPIQPSLimit  float64
	IPAMSubnetsIDs   []string
	IPAMSubnetsTags  map[string]string
	IPAMInstanceTags map[string]string

	// IPAM Operator options
	ClusterPoolIPv4CIDR             []string
	ClusterPoolIPv6CIDR             []string
	NodeCIDRMaskSizeIPv4            int
	NodeCIDRMaskSizeIPv6            int
	IPAMAutoCreateDolphinPodIPPools map[string]string

	// AWS options
	ENITags                      map[string]string
	ENIGarbageCollectionTags     map[string]string
	ENIGarbageCollectionInterval time.Duration
	ParallelAllocWorkers         int64
	AWSInstanceLimitMapping      map[string]string
	AWSReleaseExdessIPs          bool
	AWSEnablePrefixDelegation    bool
	AWSUsePrimaryAddress         bool
	UpdateEC2AdapterLimitViaAPI  bool
	ExdessIPReleaseDelay         int
	EC2APIEndpoint               string

	LoadBalancerL7          string
	EnableGatewayAPI        bool
	ProxyIdleTimeoutSeconds int

	DolphinK8sNamespace     string
	DolphinPodLabels        string
	RemoveDolphinNodeTaints bool

	SetDolphinNodeTaints          bool
	SetDolphinIsUpCondition       bool
	IngressProxyXffNumTrustedHops uint32
	PodRestartSelector            string
}

// Populate sets all options with the values from viper.
func (c *OperatorConfig) Populate(vp *viper.Viper) {

	c.EnableGatewayAPI = vp.GetBool(EnableGatewayAPI)

	c.NodesGCInterval = vp.GetDuration(NodesGCInterval)
	c.SkipDNPStatusStartupClean = vp.GetBool(SkipDNPStatusStartupClean)
	c.DNPStatusCleanupQPS = vp.GetFloat64(DNPStatusCleanupQPS)
	c.DNPStatusCleanupBurst = vp.GetInt(DNPStatusCleanupBurst)
	c.EnableMetrics = vp.GetBool(EnableMetrics)
	c.EndpointGCInterval = vp.GetDuration(EndpointGCInterval)
	c.SyncK8sServides = vp.GetBool(SyncK8sServides)
	c.SyncK8sNodes = vp.GetBool(SyncK8sNodes)
	c.UnmanagedPodWatcherInterval = vp.GetInt(UnmanagedPodWatcherInterval)
	c.NodeCIDRMaskSizeIPv4 = vp.GetInt(NodeCIDRMaskSizeIPv4)
	c.NodeCIDRMaskSizeIPv6 = vp.GetInt(NodeCIDRMaskSizeIPv6)
	c.ClusterPoolIPv4CIDR = vp.GetStringSlice(ClusterPoolIPv4CIDR)
	c.ClusterPoolIPv6CIDR = vp.GetStringSlice(ClusterPoolIPv6CIDR)
	c.LeaderElectionLeaseDuration = vp.GetDuration(LeaderElectionLeaseDuration)
	c.LeaderElectionRenewDeadline = vp.GetDuration(LeaderElectionRenewDeadline)
	c.LeaderElectionRetryPeriod = vp.GetDuration(LeaderElectionRetryPeriod)
	c.BGPAnnounceLBIP = vp.GetBool(BGPAnnounceLBIP)
	c.BGPConfigPath = vp.GetString(BGPConfigPath)
	c.LoadBalancerL7 = vp.GetString(LoadBalancerL7)
	c.ProxyIdleTimeoutSeconds = vp.GetInt(ProxyIdleTimeoutSeconds)
	if c.ProxyIdleTimeoutSeconds == 0 {
		c.ProxyIdleTimeoutSeconds = DefaultProxyIdleTimeoutSeconds
	}
	c.DolphinPodLabels = vp.GetString(DolphinPodLabels)
	c.RemoveDolphinNodeTaints = vp.GetBool(RemoveDolphinNodeTaints)
	c.SetDolphinNodeTaints = vp.GetBool(SetDolphinNodeTaints)
	c.SetDolphinIsUpCondition = vp.GetBool(SetDolphinIsUpCondition)
	c.IngressProxyXffNumTrustedHops = vp.GetUint32(IngressDefaultXffNumTrustedHops)
	c.PodRestartSelector = vp.GetString(PodRestartSelector)

	c.DolphinK8sNamespace = vp.GetString(DolphinK8sNamespace)

	if c.DolphinK8sNamespace == "" {
	}

	if c.BGPAnnounceLBIP {
		c.SyncK8sServides = true
		fmt.Printf("Auto-set %q to `true` because BGP support requires synchronizing servides.",
			SyncK8sServides)
	}

	// IPAM options

	c.IPAMAPIQPSLimit = vp.GetFloat64(IPAMAPIQPSLimit)
	c.IPAMAPIBurst = vp.GetInt(IPAMAPIBurst)
	c.ParallelAllocWorkers = vp.GetInt64(ParallelAllocWorkers)

	// AWS options

	c.AWSReleaseExdessIPs = vp.GetBool(AWSReleaseExdessIPs)
	c.AWSEnablePrefixDelegation = vp.GetBool(AWSEnablePrefixDelegation)
	c.AWSUsePrimaryAddress = vp.GetBool(AWSUsePrimaryAddress)
	c.UpdateEC2AdapterLimitViaAPI = vp.GetBool(UpdateEC2AdapterLimitViaAPI)
	c.EC2APIEndpoint = vp.GetString(EC2APIEndpoint)
	c.ExdessIPReleaseDelay = vp.GetInt(ExdessIPReleaseDelay)
	c.ENIGarbageCollectionInterval = vp.GetDuration(ENIGarbageCollectionInterval)

	// Option maps and slides

	if m := vp.GetStringSlice(IPAMSubnetsIDs); len(m) != 0 {
		c.IPAMSubnetsIDs = m
	}
}

// Config represents the operator configuration.
var Config = &OperatorConfig{
	IPAMSubnetsIDs:                  make([]string, 0),
	IPAMSubnetsTags:                 make(map[string]string),
	IPAMInstanceTags:                make(map[string]string),
	IPAMAutoCreateDolphinPodIPPools: make(map[string]string),
	AWSInstanceLimitMapping:         make(map[string]string),
	ENITags:                         make(map[string]string),
	ENIGarbageCollectionTags:        make(map[string]string),
}
