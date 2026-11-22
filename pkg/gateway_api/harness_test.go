package gateway_api

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ---------------------------------------------------------------------------
// Test data layout (relative to the package directory):
//
//	testdata/common/*.yaml            applied for every case (GatewayClass, backends)
//	testdata/cases/<any/nesting>/<case>/
//	    input/*.yaml                  objects to create (multi doc yaml is fine)
//	    expected/*.yaml               objects to verify after the reconcile
//	    case.yaml                     optional, see caseConfig
//
// Every directory that contains an input/ directory is one test case.
// ---------------------------------------------------------------------------

const (
	commonDir = "testdata/common"
	casesDir  = "testdata/cases"
)

// updateExpected regenerates expected/ from the live objects:
//
//	go test ./pkg/gateway_api -run TestControllers -update
//	ginkgo --focus "cross-protocol" -- -update
var updateExpected = flag.Bool("update", false, "rewrite testdata/cases/*/expected from the reconciled objects")

// caseConfig is the optional case.yaml. Everything has a default.
type caseConfig struct {
	// Reconcilers to run, in order. Default: every reconciler for which the
	// input contains objects, in the order referencegrant, tlsroute,
	// grpcroute, gateway. Known: gateway, tlsroute, grpcroute, referencegrant.
	// (HTTPRoute status is written by the gateway reconciler.)
	Reconcile []string `json:"reconcile,omitempty"`
	// How many times the whole list is run (statuses depend on each other).
	Rounds int `json:"rounds,omitempty"`
	// The reconcile is expected to return an error at least once.
	ExpectError bool `json:"expectError,omitempty"`
	// Namespace used for the SDS secrets of the rendered envoy config.
	SecretNamespace string `json:"secretNamespace,omitempty"`
	// CRDs the gateway reconciler believes are installed, "Kind.version.group",
	// e.g. "TCPRoute.v1alpha2.gateway.networking.k8s.io".
	InstalledCRDs []string `json:"installedCRDs,omitempty"`
	// Extra text replacements applied to the expected files before comparing.
	ExpectedRewrites map[string]string `json:"expectedRewrites,omitempty"`
}

type testCase struct {
	Name   string // path relative to testdata/cases, used as the spec name
	Dir    string
	Config caseConfig
}

// discoverCases returns every directory below root that has an input/ directory.
func discoverCases(root string) ([]testCase, error) {
	var cases []testCase
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if st, statErr := os.Stat(filepath.Join(path, "input")); statErr == nil && st.IsDir() {
			rel, _ := filepath.Rel(root, path)
			tc := testCase{Name: filepath.ToSlash(rel), Dir: path}
			if b, readErr := os.ReadFile(filepath.Join(path, "case.yaml")); readErr == nil {
				if uErr := yaml.UnmarshalStrict(b, &tc.Config); uErr != nil {
					return fmt.Errorf("%s/case.yaml: %w", path, uErr)
				}
			}
			if tc.Config.Rounds == 0 {
				tc.Config.Rounds = 2
			}
			if tc.Config.SecretNamespace == "" {
				tc.Config.SecretNamespace = "cilium-secrets"
			}
			cases = append(cases, tc)
			return filepath.SkipDir
		}
		return nil
	})
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, err
}

// ---------------------------------------------------------------------------
// yaml loading
// ---------------------------------------------------------------------------

func readYAMLFile(path string) []*unstructured.Unstructured {
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var out []*unstructured.Unstructured
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(b)))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred(), "reading %s", path)

		u := &unstructured.Unstructured{}
		Expect(yaml.Unmarshal(doc, &u.Object)).To(Succeed(), "parsing %s", path)
		if len(u.Object) == 0 { // comment only / empty document
			continue
		}
		Expect(u.GetKind()).NotTo(BeEmpty(), "%s: document without kind", path)
		out = append(out, u)
	}
	return out
}

// readYAMLDir reads every *.yaml / *.yml of dir (sorted by file name).
// A missing directory yields no documents.
func readYAMLDir(dir string) []*unstructured.Unstructured {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	Expect(err).NotTo(HaveOccurred())

	var names []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []*unstructured.Unstructured
	for _, n := range names {
		out = append(out, readYAMLFile(filepath.Join(dir, n))...)
	}
	return out
}

// ---------------------------------------------------------------------------
// mkcert
// ---------------------------------------------------------------------------
var (
	mkcertBinOnce sync.Once
	mkcertBin     string
)

// findMkcert: PATH -> $(go env GOPATH)/bin -> `go install filippo.io/mkcert` into a temp GOBIN.
func findMkcert() string {
	mkcertBinOnce.Do(func() {
		if p, err := exec.LookPath("mkcert"); err == nil {
			mkcertBin = p
			return
		}
		if gp, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
			p := filepath.Join(strings.TrimSpace(string(gp)), "bin", "mkcert")
			if _, statErr := os.Stat(p); statErr == nil {
				mkcertBin = p
				return
			}
		}
		binDir, err := os.MkdirTemp("", "gwapi-mkcert-bin-")
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command("go", "install", "filippo.io/mkcert@v1.4.4")
		cmd.Env = append(os.Environ(), "GOBIN="+binDir)
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "go install mkcert failed: %s", string(out))
		mkcertBin = filepath.Join(binDir, "mkcert")
	})
	return mkcertBin
}

var (
	mkcertMu    sync.Mutex
	mkcertDir   string
	mkcertCache = map[string][2]string{} // hosts -> {cert pem, key pem}
)

// mkcertFor returns a PEM certificate and key for the hosts, generated with
// mkcert. A throw-away CA root is used: nothing is installed in any trust store.
func mkcertFor(hosts []string) (certPEM, keyPEM string) {
	mkcertMu.Lock()
	defer mkcertMu.Unlock()

	cacheKey := strings.Join(hosts, ",")
	if v, ok := mkcertCache[cacheKey]; ok {
		return v[0], v[1]
	}

	bin := findMkcert()
	var err error
	Expect(err).NotTo(HaveOccurred(), "mkcert not found in PATH (https://github.com/FiloSottile/mkcert)")

	if mkcertDir == "" {
		mkcertDir, err = os.MkdirTemp("", "gwapi-mkcert-")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(mkcertDir, "caroot"), 0o700)).To(Succeed())
	}

	n := len(mkcertCache)
	certFile := filepath.Join(mkcertDir, fmt.Sprintf("cert-%d.pem", n))
	keyFile := filepath.Join(mkcertDir, fmt.Sprintf("key-%d.pem", n))

	args := append([]string{"-cert-file", certFile, "-key-file", keyFile}, hosts...)
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "CAROOT="+filepath.Join(mkcertDir, "caroot"))
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "mkcert %v failed: %s", hosts, string(out))

	crt, err := os.ReadFile(certFile)
	Expect(err).NotTo(HaveOccurred())
	key, err := os.ReadFile(keyFile)
	Expect(err).NotTo(HaveOccurred())

	mkcertCache[cacheKey] = [2]string{string(crt), string(key)}
	return string(crt), string(key)
}

// cleanupMkcert is called from AfterSuite.
func cleanupMkcert() {
	mkcertMu.Lock()
	defer mkcertMu.Unlock()
	if mkcertDir != "" {
		_ = os.RemoveAll(mkcertDir)
		mkcertDir = ""
	}
}

const mkcertAnnotation = "test.dolphin.io/mkcert" // optional: comma separated hosts

// fillTLSSecret turns a Secret without data into a kubernetes.io/tls Secret
// holding an mkcert generated certificate and key.
//
// Hosts come from the annotation test.dolphin.io/mkcert, otherwise from the
// hostnames of the listeners (of Gateways / ListenerSets in docs) whose
// certificateRefs point at this Secret. A Secret that already has data, or that
// nobody references, is left alone.
func fillTLSSecret(secret *unstructured.Unstructured, docs []*unstructured.Unstructured) {
	if _, ok, _ := unstructured.NestedMap(secret.Object, "data"); ok {
		return
	}
	if _, ok, _ := unstructured.NestedMap(secret.Object, "stringData"); ok {
		return
	}

	var hosts []string
	if ann := secret.GetAnnotations()[mkcertAnnotation]; ann != "" {
		for _, h := range strings.Split(ann, ",") {
			hosts = append(hosts, strings.TrimSpace(h))
		}
		remaining := secret.GetAnnotations()
		delete(remaining, mkcertAnnotation)
		secret.SetAnnotations(remaining)
	} else {
		referenced := false
		set := map[string]struct{}{}
		for _, d := range docs {
			if d.GetKind() != "Gateway" && d.GetKind() != "ListenerSet" {
				continue
			}
			listeners, _, _ := unstructured.NestedSlice(d.Object, "spec", "listeners")
			for _, l := range listeners {
				lm, _ := l.(map[string]any)
				refs, _, _ := unstructured.NestedSlice(lm, "tls", "certificateRefs")
				for _, r := range refs {
					rm, _ := r.(map[string]any)
					kind, _ := rm["kind"].(string)
					name, _ := rm["name"].(string)
					ns, _ := rm["namespace"].(string)
					if ns == "" {
						ns = d.GetNamespace()
					}
					if (kind == "" || kind == "Secret") && name == secret.GetName() && ns == secret.GetNamespace() {
						referenced = true
						if h, _ := lm["hostname"].(string); h != "" {
							set[h] = struct{}{}
						}
					}
				}
			}
		}
		if !referenced {
			return
		}
		for h := range set {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		if len(hosts) == 0 {
			hosts = []string{"localhost"}
		}
	}

	crt, key := mkcertFor(hosts)
	Expect(unstructured.SetNestedField(secret.Object, string(corev1.SecretTypeTLS), "type")).To(Succeed())
	Expect(unstructured.SetNestedStringMap(secret.Object, map[string]string{
		corev1.TLSCertKey:       crt,
		corev1.TLSPrivateKeyKey: key,
	}, "stringData")).To(Succeed())
}

// ---------------------------------------------------------------------------
// applying input
// ---------------------------------------------------------------------------

func isNamespaced(u *unstructured.Unstructured) bool { return u.GetNamespace() != "" }

func ensureNamespaces(ctx context.Context, docs []*unstructured.Unstructured) {
	seen := map[string]struct{}{}
	for _, d := range docs {
		ns := d.GetNamespace()
		if ns == "" || d.GetKind() == "Namespace" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		// namespaces are never deleted (no namespace controller in envtest)
		err := caseClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

// applyDocs creates the documents. Differences to `kubectl apply` that the test
// data relies on:
//   - Secrets without data are filled with an mkcert certificate (fillTLSSecret);
//   - Services without ports get a placeholder port (the API server rejects
//     them; a controller replaces the spec anyway);
//   - a `status` in the document is written through the status subresource.
func applyDocs(ctx context.Context, docs []*unstructured.Unstructured) {
	ensureNamespaces(ctx, docs)

	for _, d := range docs {
		u := d.DeepCopy()

		switch u.GetKind() {
		case "Secret":
			fillTLSSecret(u, docs)
		case "Service":
			typ, _, _ := unstructured.NestedString(u.Object, "spec", "type")
			ports, _, _ := unstructured.NestedSlice(u.Object, "spec", "ports")
			if len(ports) == 0 && typ != "ExternalName" {
				Expect(unstructured.SetNestedSlice(u.Object, []any{
					map[string]any{"name": "placeholder", "port": int64(1), "protocol": "TCP"},
				}, "spec", "ports")).To(Succeed())
			}
		}

		status, hasStatus := u.Object["status"]
		delete(u.Object, "status")

		err := caseClient.Create(ctx, u)
		if u.GetKind() == "Namespace" && apierrors.IsAlreadyExists(err) {
			// created by ensureNamespaces or by an earlier case; namespaces are never deleted in envtest
			continue
		}
		Expect(err).NotTo(HaveOccurred(), "creating %s %s/%s", u.GetKind(), u.GetNamespace(), u.GetName())

		if hasStatus {
			u.Object["status"] = status
			Expect(caseClient.Status().Update(ctx, u)).To(Succeed(), "setting status of %s %s/%s", u.GetKind(), u.GetNamespace(), u.GetName())
		}
	}
}

// ---------------------------------------------------------------------------
// cleanup between cases
// ---------------------------------------------------------------------------

func mkGVK(group, version, kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
}

// every namespaced kind a case may leave behind (input + generated by the controller)
var cleanupKinds = []schema.GroupVersionKind{
	mkGVK("gateway.networking.k8s.io", "v1", "Gateway"),
	mkGVK("gateway.networking.k8s.io", "v1", "HTTPRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "TLSRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "GRPCRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "ListenerSet"),
	mkGVK("gateway.networking.k8s.io", "v1", "BackendTLSPolicy"),
	mkGVK("gateway.networking.k8s.io", "v1beta1", "ReferenceGrant"),
	mkGVK("dolphin.io", "v1", "DolphinEnvoyConfig"),
	mkGVK("cilium.io", "v2", "CiliumEnvoyConfig"),
	mkGVK("", "v1", "Service"),
	mkGVK("", "v1", "Endpoints"),
	mkGVK("", "v1", "Secret"),
	mkGVK("", "v1", "ConfigMap"),
	mkGVK("apps", "v1", "Deployment"),
}

func purge(ctx context.Context, kind schema.GroupVersionKind, opts ...client.ListOption) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(kind.GroupVersion().WithKind(kind.Kind + "List"))
	Expect(caseClient.List(ctx, list, opts...)).To(Succeed())
	for i := range list.Items {
		item := &list.Items[i]
		if len(item.GetFinalizers()) > 0 {
			patch := client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`))
			Expect(client.IgnoreNotFound(caseClient.Patch(ctx, item, patch))).To(Succeed())
		}
		Expect(client.IgnoreNotFound(caseClient.Delete(ctx, item))).To(Succeed())
	}
}

// cleanupCase removes everything the case created or caused to be created, so
// the next case starts from an empty cluster (namespaces and CRDs stay).
func cleanupCase(ctx context.Context, docs []*unstructured.Unstructured) {
	namespaces := map[string]struct{}{}
	for _, d := range docs {
		if d.GetNamespace() != "" {
			namespaces[d.GetNamespace()] = struct{}{}
		}
	}
	for ns := range namespaces {
		for _, k := range cleanupKinds {
			purge(ctx, k, client.InNamespace(ns))
		}
	}
	// cluster scoped objects of the case (GatewayClass)
	for _, d := range docs {
		if !isNamespaced(d) && d.GetKind() != "Namespace" {
			Expect(client.IgnoreNotFound(caseClient.Delete(ctx, d.DeepCopy()))).To(Succeed())
		}
	}
}

// ---------------------------------------------------------------------------
// reconcile
// ---------------------------------------------------------------------------

type reconcileFn func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

type reconcilerEntry struct {
	kind string // kind of the input objects the reconciler is driven with
	fn   reconcileFn
}

// defaultReconcileOrder: route level reconcilers first, the gateway last.
var defaultReconcileOrder = []string{"referencegrant", "tlsroute", "grpcroute", "gateway"}

func buildReconcilers(tc testCase) map[string]reconcilerEntry {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var installed []schema.GroupVersionKind
	for _, s := range tc.Config.InstalledCRDs {
		g, _ := schema.ParseKindArg(s)
		Expect(g).NotTo(BeNil(), "case.yaml installedCRDs: bad entry %q, want Kind.version.group", s)
		installed = append(installed, *g)
	}

	gw := &gatewayReconciler{
		Client:             caseClient,
		Scheme:             caseScheme,
		APIReader:          caseClient,
		SecretNamespace:    tc.Config.SecretNamespace,
		IdleTimeoutSeconds: 60,
		EnableIPv4:         true,
		logger:             logger,
		controllerName:     controllerName,
		installedCRDs:      installed,
	}
	gw.routeStatusManager = NewRouteStatusManager(caseClient, logger, controllerName)

	return map[string]reconcilerEntry{
		"gateway":        {"Gateway", gw.Reconcile},
		"tlsroute":       {"TLSRoute", (&tlsrouteReconciler{Client: caseClient, Scheme: caseScheme}).Reconcile},
		"grpcroute":      {"GRPCRoute", (&grpcrouteReconciler{Client: caseClient, Scheme: caseScheme}).Reconcile},
		"referencegrant": {"ReferenceGrant", (&referencegrantReconciler{Client: caseClient, Scheme: caseScheme}).Reconcile},
	}
}

// runReconcilers drives the reconcilers with a request for every input object
// of the kind they handle, like the controller-runtime watches would.
func runReconcilers(ctx context.Context, tc testCase, input []*unstructured.Unstructured) {
	reconcilers := buildReconcilers(tc)

	order := tc.Config.Reconcile
	if len(order) == 0 {
		order = defaultReconcileOrder
	}

	var errs []error
	for round := 1; round <= tc.Config.Rounds; round++ {
		for _, name := range order {
			r, ok := reconcilers[name]
			Expect(ok).To(BeTrue(), "unknown reconciler %q", name)

			for _, d := range input {
				if d.GetKind() != r.kind {
					continue
				}
				req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: d.GetNamespace(), Name: d.GetName()}}
				_, err := r.fn(ctx, req)
				if err != nil {
					errs = append(errs, err)
					if !tc.Config.ExpectError {
						Expect(err).NotTo(HaveOccurred(), "reconciler %s, %s %s, round %d", name, r.kind, req.NamespacedName, round)
					}
				}
			}
		}
	}
	if tc.Config.ExpectError {
		Expect(errs).NotTo(BeEmpty(), "case.yaml says expectError, but no reconcile returned an error")
	}
}

// ---------------------------------------------------------------------------
// comparing
// ---------------------------------------------------------------------------

// normalize drops volatile / generation dependent fields and makes condition
// lists order independent.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = normalize(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = normalize(x)
		}
		sortConditions(out)
		return out
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case float32:
		return float64(t)
	case uint32:
		return float64(t)
	case uint64:
		return float64(t)
	default:
		return v
	}
}

func sortConditions(list []any) {
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return
		}
		if _, ok := m["type"].(string); !ok {
			return
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].(map[string]any)["type"].(string) <
			list[j].(map[string]any)["type"].(string)
	})
}

// subsetDiff reports every place where `got` does not contain `want`:
//   - objects: every key of want must exist in got (extra keys in got are fine:
//     the API server adds CRD defaults such as group/kind/weight);
//   - lists: same length, element by element;
//   - scalars: equal.
func subsetDiff(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want object %v, got %v", path, w, got)}
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []string
		for _, k := range keys {
			gv, ok := g[k]
			if !ok {
				out = append(out, fmt.Sprintf("%s.%s: missing, want %v", path, k, w[k]))
				continue
			}
			out = append(out, subsetDiff(path+"."+k, w[k], gv)...)
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want list %v, got %v", path, w, got)}
		}
		if len(w) != len(g) {
			return []string{fmt.Sprintf("%s: want %d items, got %d\n    want: %v\n    got:  %v", path, len(w), len(g), w, g)}
		}
		var out []string
		for i := range w {
			out = append(out, subsetDiff(fmt.Sprintf("%s[%d]", path, i), w[i], g[i])...)
		}
		return out
	default:
		if !reflect.DeepEqual(want, got) {
			return []string{fmt.Sprintf("%s: want %v, got %v", path, want, got)}
		}
		return nil
	}
}

// rewriteExpected applies the cilium -> dolphin renames to an expected
// document. Only names are touched, routes stay exactly as they are.
func rewriteExpected(tc testCase, doc *unstructured.Unstructured) *unstructured.Unstructured {
	b, err := yaml.Marshal(doc.Object)
	Expect(err).NotTo(HaveOccurred())

	pairs := []string{
		"gatewayClassName: cilium", "gatewayClassName: dolphin",
		"io.cilium/gateway-controller", controllerName,
		"cilium-gateway-", "dolphin-gateway-",
	}
	keys := make([]string, 0, len(tc.Config.ExpectedRewrites))
	for k := range tc.Config.ExpectedRewrites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pairs = append(pairs, k, tc.Config.ExpectedRewrites[k])
	}
	b = []byte(strings.NewReplacer(pairs...).Replace(string(b)))

	out := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(b, &out.Object)).To(Succeed())
	return out
}

// strictGroups: the rendered envoy configs have no API server defaults, so the
// whole spec must match exactly (both directions).
var strictGroups = map[string]bool{"dolphin.io": true, "cilium.io": true}

// compareObject returns the differences between the expected document and the
// live object. Compared: every top level field except apiVersion/kind/metadata
// (spec, status, data, ...), plus metadata.labels / annotations / ownerReferences.
func compareObject(want, got *unstructured.Unstructured) []string {
	var diffs []string
	strict := strictGroups[want.GroupVersionKind().Group]

	keys := make([]string, 0, len(want.Object))
	for k := range want.Object {
		if k == "apiVersion" || k == "kind" || k == "metadata" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w := normalize(want.Object[k])
		g := normalize(got.Object[k])
		diffs = append(diffs, subsetDiff(k, w, g)...)
		if strict {
			diffs = append(diffs, subsetDiff(k+" (unexpected extra)", g, w)...)
		}
	}

	wm, _ := want.Object["metadata"].(map[string]any)
	gm, _ := got.Object["metadata"].(map[string]any)
	for _, k := range []string{"labels", "annotations", "ownerReferences"} {
		if wv, ok := wm[k]; ok {
			diffs = append(diffs, subsetDiff("metadata."+k, wv, gm[k])...)
		}
	}
	return diffs
}

func verifyExpected(ctx context.Context, tc testCase) {
	wants := readYAMLDir(filepath.Join(tc.Dir, "expected"))
	Expect(wants).NotTo(BeEmpty(), "%s: expected/ has no documents (run with -update to create them)", tc.Name)

	var failures []string
	for _, w := range wants {
		w = rewriteExpected(tc, w)

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(w.GroupVersionKind())
		key := types.NamespacedName{Namespace: w.GetNamespace(), Name: w.GetName()}
		if err := caseClient.Get(ctx, key, got); err != nil {
			failures = append(failures, fmt.Sprintf("%s %s: cannot get object: %v", w.GetKind(), key, err))
			continue
		}
		for _, d := range compareObject(w, got) {
			failures = append(failures, fmt.Sprintf("%s %s: %s", w.GetKind(), key, d))
		}
	}
	Expect(failures).To(BeEmpty(), "\n"+strings.Join(failures, "\n"))
}

// ---------------------------------------------------------------------------
// -update: snapshot of the reconciled objects
// ---------------------------------------------------------------------------

var snapshotKinds = []schema.GroupVersionKind{
	mkGVK("gateway.networking.k8s.io", "v1", "Gateway"),
	mkGVK("gateway.networking.k8s.io", "v1", "HTTPRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "TLSRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "GRPCRoute"),
	mkGVK("gateway.networking.k8s.io", "v1", "ListenerSet"),
	mkGVK("dolphin.io", "v1", "DolphinEnvoyConfig"),
	mkGVK("cilium.io", "v2", "CiliumEnvoyConfig"),
}

// writeExpected replaces expected/ with a normalized snapshot of every
// snapshotKinds object in the namespaces of the input. Review the git diff.
func writeExpected(ctx context.Context, tc testCase, input []*unstructured.Unstructured) {
	dir := filepath.Join(tc.Dir, "expected")
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	old, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	for _, f := range old {
		Expect(os.Remove(f)).To(Succeed())
	}

	namespaces := map[string]struct{}{}
	for _, d := range input {
		if d.GetNamespace() != "" {
			namespaces[d.GetNamespace()] = struct{}{}
		}
	}

	for ns := range namespaces {
		for _, k := range snapshotKinds {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(k.GroupVersion().WithKind(k.Kind + "List"))
			Expect(caseClient.List(ctx, list, client.InNamespace(ns))).To(Succeed())

			for i := range list.Items {
				item := &list.Items[i]
				snap := map[string]any{
					"apiVersion": item.GetAPIVersion(),
					"kind":       item.GetKind(),
				}
				meta := map[string]any{"name": item.GetName(), "namespace": item.GetNamespace()}
				if l := item.GetLabels(); len(l) > 0 {
					meta["labels"] = toAnyMap(l)
				}
				if a := item.GetAnnotations(); len(a) > 0 {
					meta["annotations"] = toAnyMap(a)
				}
				if o, ok, _ := unstructured.NestedSlice(item.Object, "metadata", "ownerReferences"); ok {
					for _, r := range o {
						if rm, ok := r.(map[string]any); ok {
							delete(rm, "uid")
						}
					}
					meta["ownerReferences"] = o
				}
				snap["metadata"] = meta
				for _, f := range []string{"spec", "status"} {
					if v, ok := item.Object[f]; ok {
						snap[f] = normalize(v)
					}
				}

				b, err := yaml.Marshal(snap)
				Expect(err).NotTo(HaveOccurred())
				name := fmt.Sprintf("%s-%s.yaml", strings.ToLower(item.GetKind()), item.GetName())
				Expect(os.WriteFile(filepath.Join(dir, name), b, 0o644)).To(Succeed())
			}
		}
	}
}

func toAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// one case
// ---------------------------------------------------------------------------

func runCase(ctx context.Context, tc testCase) {
	By("loading common + input yaml")
	common := readYAMLDir(commonDir)
	input := readYAMLDir(filepath.Join(tc.Dir, "input"))
	Expect(input).NotTo(BeEmpty(), "%s: input/ has no documents", tc.Name)
	all := append(append([]*unstructured.Unstructured{}, common...), input...)

	// registered first, so a half applied case is cleaned up as well
	DeferCleanup(func() { cleanupCase(context.Background(), all) })

	By("creating the objects (mkcert secrets, LB service status)")
	applyDocs(ctx, all)

	By("running the reconcilers")
	runReconcilers(ctx, tc, input)

	if *updateExpected {
		By("-update: rewriting expected/")
		writeExpected(ctx, tc, input)
		return
	}

	By("verifying expected/")
	verifyExpected(ctx, tc)
}
