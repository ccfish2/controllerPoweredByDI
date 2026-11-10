package ingress

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	envoy_config_cluster_v3 "github.com/cilium/proxy/go/envoy/config/cluster/v3"
	envoy_config_route_v3 "github.com/cilium/proxy/go/envoy/config/route/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utiljson "k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/ccfish2/controllerPoweredByDI/pkg/envoy"
	dolphinv1 "github.com/ccfish2/infra/pkg/k8s/apis/dolphin.io/v1"

	"github.com/ccfish2/infra/pkg/k8s"
)

const (
	// ciliumUseOriginalSourceAddressLabel is the label cilium-agent looks at on a
	// CiliumEnvoyConfig. It is the cilium counterpart of
	// k8s.UseOriginalSourceAddressLabel ("dolphin.io/use-original-source-address").
	ciliumUseOriginalSourceAddressLabel = "cilium.io/use-original-source-address"
)

var ciliumEnvoyConfigGVK = schema.GroupVersionKind{
	Group:   "cilium.io",
	Version: "v2",
	Kind:    "CiliumEnvoyConfig",
}

// newCiliumEnvoyConfig returns an empty CiliumEnvoyConfig. The type is handled
// as unstructured, so we don't need to pull cilium's API package into the
// operator (same approach as gateway_api.ensureCiliumEnvoyConfig).
func newCiliumEnvoyConfig(namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(ciliumEnvoyConfigGVK)
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

// createOrUpdateEnvoyConfigs persists the generated DolphinEnvoyConfig and then
// the CiliumEnvoyConfig derived from it. cilium-envoy only consumes the latter.
//
// If you don't want to keep the DolphinEnvoyConfig around any more, drop the
// first call here (and the Owns/Watches on DolphinEnvoyConfig in ingress.go).
func (r *ingressReconciler) createOrUpdateEnvoyConfigs(ctx context.Context, desired *dolphinv1.DolphinEnvoyConfig) error {
	if desired == nil {
		return fmt.Errorf("desired DolphinEnvoyConfig is nil")
	}

	if err := r.createOrUpdateDolphinEnvoyConfig(ctx, desired); err != nil {
		return err
	}

	return r.createOrUpdateCiliumEnvoyConfig(ctx, desired)
}

func (r *ingressReconciler) createOrUpdateCiliumEnvoyConfig(ctx context.Context, dec *dolphinv1.DolphinEnvoyConfig) error {
	// Work on a copy: the DolphinEnvoyConfig that was/will be persisted must not
	// be touched by the fix-ups below.
	fixed := dec.DeepCopy()
	if err := fixDolphinEnvoyConfig(fixed); err != nil {
		return fmt.Errorf("failed to fix DolphinEnvoyConfig %s/%s: %w", dec.Namespace, dec.Name, err)
	}

	desired, err := toCiliumEnvoyConfig(fixed)
	if err != nil {
		return fmt.Errorf("failed to convert DolphinEnvoyConfig %s/%s into CiliumEnvoyConfig: %w", dec.Namespace, dec.Name, err)
	}

	// A CiliumEnvoyConfig without any xDS resource is meaningless (this happens
	// for the shared config when the last shared Ingress is gone), so remove it.
	resources, _, _ := unstructured.NestedSlice(desired.Object, "spec", "resources")
	if len(resources) == 0 {
		return r.tryDeletingResource(ctx,
			newCiliumEnvoyConfig(desired.GetNamespace(), desired.GetName()),
			types.NamespacedName{Namespace: desired.GetNamespace(), Name: desired.GetName()})
	}

	cec := newCiliumEnvoyConfig(desired.GetNamespace(), desired.GetName())
	result, err := controllerutil.CreateOrUpdate(ctx, r.client, cec, func() error {
		if err := unstructured.SetNestedField(cec.Object, desired.Object["spec"], "spec"); err != nil {
			return err
		}
		cec.SetOwnerReferences(desired.GetOwnerReferences())
		cec.SetLabels(desired.GetLabels())
		cec.SetAnnotations(desired.GetAnnotations())
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update CiliumEnvoyConfig: %w", err)
	}
	r.logger.Debugf("CiliumEnvoyConfig %s has been %s", client.ObjectKeyFromObject(cec), result)

	return nil
}

// ---------------------------------------------------------------------------
// Fixing the DolphinEnvoyConfig
// ---------------------------------------------------------------------------

type edsService struct {
	namespace, name, port string
}

// fixDolphinEnvoyConfig repairs inconsistencies that make cilium-envoy reject or
// silently break a config that Envoy itself would never accept:
//
//  1. a route that points at a cluster which does not exist
//     (e.g. route -> "dolphin:details:9080" while the cluster is called
//     "details:details:9080"). It is re-pointed to the one cluster serving the
//     same "<service>:<port>"; if that is ambiguous or impossible an error is
//     returned.
//  2. an EDS cluster whose service is missing from spec.backendServices. cilium
//     only fills the EDS cluster with endpoints for backendServices.
//
// It works on the in-memory object produced by the translator, NOT on the object
// read back from the API server (see the note about socketOptions in the docs).
func fixDolphinEnvoyConfig(dec *dolphinv1.DolphinEnvoyConfig) error {
	clusterNames, eds, err := collectClusters(dec.Spec.Resources)
	if err != nil {
		return err
	}

	for i, res := range dec.Spec.Resources {
		if res.Any == nil || res.Any.GetTypeUrl() != envoy.RouteTypeURL {
			continue
		}

		rc := &envoy_config_route_v3.RouteConfiguration{}
		if err := res.Any.UnmarshalTo(rc); err != nil {
			return fmt.Errorf("failed to decode RouteConfiguration: %w", err)
		}

		changed, err := fixRouteClusterReferences(rc, clusterNames)
		if err != nil {
			return fmt.Errorf("route configuration %q: %w", rc.GetName(), err)
		}
		if !changed {
			continue
		}

		fixed := &anypb.Any{}
		if err := anypb.MarshalFrom(fixed, rc, proto.MarshalOptions{Deterministic: true}); err != nil {
			return fmt.Errorf("failed to encode RouteConfiguration %q: %w", rc.GetName(), err)
		}
		dec.Spec.Resources[i] = dolphinv1.XDSResource{Any: fixed}
	}

	dec.Spec.BackendServices = ensureBackendServices(dec.Spec.BackendServices, eds)

	return nil
}

func collectClusters(resources []dolphinv1.XDSResource) (map[string]struct{}, []edsService, error) {
	names := map[string]struct{}{}
	var services []edsService

	for _, res := range resources {
		if res.Any == nil || res.Any.GetTypeUrl() != envoy.ClusterTypeURL {
			continue
		}
		c := &envoy_config_cluster_v3.Cluster{}
		if err := res.Any.UnmarshalTo(c); err != nil {
			return nil, nil, fmt.Errorf("failed to decode Cluster: %w", err)
		}
		names[c.GetName()] = struct{}{}

		if ns, name, port, ok := parseEDSServiceName(c.GetEdsClusterConfig().GetServiceName()); ok {
			services = append(services, edsService{namespace: ns, name: name, port: port})
		}
	}

	return names, services, nil
}

// parseEDSServiceName parses "<namespace>/<name>:<port>".
func parseEDSServiceName(s string) (namespace, name, port string, ok bool) {
	slash := strings.Index(s, "/")
	colon := strings.LastIndex(s, ":")
	if slash <= 0 || colon <= slash+1 || colon == len(s)-1 {
		return "", "", "", false
	}
	return s[:slash], s[slash+1 : colon], s[colon+1:], true
}

func fixRouteClusterReferences(rc *envoy_config_route_v3.RouteConfiguration, known map[string]struct{}) (bool, error) {
	changed := false

	rewrite := func(old string) (string, error) {
		if old == "" {
			return old, nil
		}
		if _, ok := known[old]; ok {
			return old, nil
		}
		fixed, err := matchCluster(old, known)
		if err != nil {
			return "", err
		}
		changed = true
		return fixed, nil
	}

	for _, vh := range rc.GetVirtualHosts() {
		for _, route := range vh.GetRoutes() {
			action := route.GetRoute()
			if action == nil {
				// redirect / direct response, no cluster involved
				continue
			}

			switch spec := action.GetClusterSpecifier().(type) {
			case *envoy_config_route_v3.RouteAction_Cluster:
				n, err := rewrite(spec.Cluster)
				if err != nil {
					return false, err
				}
				spec.Cluster = n
			case *envoy_config_route_v3.RouteAction_WeightedClusters:
				for _, c := range spec.WeightedClusters.GetClusters() {
					n, err := rewrite(c.GetName())
					if err != nil {
						return false, err
					}
					c.Name = n
				}
			}

			for _, m := range action.GetRequestMirrorPolicies() {
				n, err := rewrite(m.GetCluster())
				if err != nil {
					return false, err
				}
				m.Cluster = n
			}
		}
	}

	return changed, nil
}

// matchCluster finds the single known cluster with the same "<service>:<port>"
// tail as name ("<namespace>:<service>:<port>").
func matchCluster(name string, known map[string]struct{}) (string, error) {
	tail := clusterTail(name)

	var matches []string
	for k := range known {
		if clusterTail(k) == tail {
			matches = append(matches, k)
		}
	}
	sort.Strings(matches)

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("route references unknown cluster %q and no cluster serves %q", name, tail)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("route references unknown cluster %q and it is ambiguous between %v", name, matches)
	}
}

func clusterTail(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 3 {
		return name
	}
	return strings.Join(parts[len(parts)-2:], ":")
}

func ensureBackendServices(existing []*dolphinv1.Service, eds []edsService) []*dolphinv1.Service {
	type key struct{ namespace, name string }
	byKey := map[key]*dolphinv1.Service{}

	for _, s := range existing {
		if s == nil {
			continue
		}
		k := key{s.Namespace, s.Name}
		if cur, ok := byKey[k]; ok {
			cur.Ports = append(cur.Ports, s.Ports...)
			continue
		}
		byKey[k] = s
	}

	for _, e := range eds {
		k := key{e.namespace, e.name}
		cur, ok := byKey[k]
		if !ok {
			cur = &dolphinv1.Service{Name: e.name, Namespace: e.namespace}
			byKey[k] = cur
		}
		cur.Ports = append(cur.Ports, e.port)
	}

	if len(byKey) == 0 {
		return existing
	}

	out := make([]*dolphinv1.Service, 0, len(byKey))
	for _, s := range byKey {
		slices.Sort(s.Ports)
		s.Ports = slices.Compact(s.Ports)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})

	return out
}

// ---------------------------------------------------------------------------
// DolphinEnvoyConfig -> CiliumEnvoyConfig
// ---------------------------------------------------------------------------

// toCiliumEnvoyConfig converts a DolphinEnvoyConfig into a CiliumEnvoyConfig
// with the same name, namespace and owner references. The two specs are
// structurally identical (services / backendServices / resources /
// nodeSelector), so this is a field-by-field copy; the only non-trivial parts are
//   - xDS resources, which are rendered with protojson (the "@type" form
//     cilium expects), and
//   - the use-original-source-address label which has a different key.
//
// Everything put into the returned object is an unstructured-safe type
// (map[string]any, []any, string, int64) so that DeepCopy/DeepEqual on it
// behave and a re-reconcile with the same input is a no-op against the API.
func toCiliumEnvoyConfig(dec *dolphinv1.DolphinEnvoyConfig) (*unstructured.Unstructured, error) {
	spec := map[string]any{}

	if len(dec.Spec.Services) > 0 {
		services := make([]any, 0, len(dec.Spec.Services))
		for _, s := range dec.Spec.Services {
			if s == nil {
				continue
			}
			svc := map[string]any{"name": s.Name}
			if s.Namespace != "" {
				svc["namespace"] = s.Namespace
			}
			if s.Listener != "" {
				svc["listener"] = s.Listener
			}
			if len(s.Ports) > 0 {
				ports := make([]any, 0, len(s.Ports))
				for _, p := range s.Ports {
					ports = append(ports, int64(p))
				}
				svc["ports"] = ports
			}
			services = append(services, svc)
		}
		spec["services"] = services
	}

	if len(dec.Spec.BackendServices) > 0 {
		backends := make([]any, 0, len(dec.Spec.BackendServices))
		for _, s := range dec.Spec.BackendServices {
			if s == nil {
				continue
			}
			b := map[string]any{"name": s.Name}
			if s.Namespace != "" {
				b["namespace"] = s.Namespace
			}
			if len(s.Ports) > 0 {
				numbers := make([]any, 0, len(s.Ports))
				for _, p := range s.Ports {
					numbers = append(numbers, p)
				}
				b["number"] = numbers
			}
			backends = append(backends, b)
		}
		spec["backendServices"] = backends
	}

	if len(dec.Spec.Resources) > 0 {
		resources := make([]any, 0, len(dec.Spec.Resources))
		for i, res := range dec.Spec.Resources {
			if res.Any == nil {
				return nil, fmt.Errorf("resource %d is empty", i)
			}
			raw, err := protojson.Marshal(res.Any)
			if err != nil {
				return nil, fmt.Errorf("failed to encode resource %d (%s): %w", i, res.Any.GetTypeUrl(), err)
			}
			// apimachinery's json package keeps whole numbers as int64 instead of float64
			var decoded map[string]any
			if err := utiljson.Unmarshal(raw, &decoded); err != nil {
				return nil, fmt.Errorf("failed to decode resource %d (%s): %w", i, res.Any.GetTypeUrl(), err)
			}
			resources = append(resources, decoded)
		}
		spec["resources"] = resources
	}

	if dec.Spec.NodeSelector != nil {
		selector, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dec.Spec.NodeSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to encode nodeSelector: %w", err)
		}
		spec["nodeSelector"] = selector
	}

	labels := make(map[string]string, len(dec.Labels))
	for k, v := range dec.Labels {
		if k == k8s.UseOriginalSourceAddressLabel {
			k = ciliumUseOriginalSourceAddressLabel
		}
		labels[k] = v
	}

	cec := newCiliumEnvoyConfig(dec.Namespace, dec.Name)
	cec.SetLabels(labels)
	cec.SetAnnotations(dec.Annotations)
	cec.SetOwnerReferences(dec.OwnerReferences)
	cec.Object["spec"] = spec

	return cec, nil
}
