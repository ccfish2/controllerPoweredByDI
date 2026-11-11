#!/usr/bin/env bash
# filepath: .github/actions/tests/kindenv/lib/metallb.sh
set -uo pipefail

NS=metallb-system
WAIT="${WAIT_SCALE:-240}"   # base timeout in seconds; raise if CI is slow

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/helper.sh"

dump_diag() {
  echo "----- diagnostics -----"
  kubectl get pods,svc,ep -A -o wide | head -80
  kubectl -n $NS describe pods | tail -60
  kubectl -n $NS logs deploy/controller --tail=50 || true
  kubectl -n $NS logs ds/speaker --tail=50 || true
  kubectl -n kube-system logs -l k8s-app=kube-dns --tail=30 || true
}
fail() { echo "❌ $*"; dump_diag; exit 1; }

# --- IP range ---------------------------------------------------------------
subnet=$(docker network inspect kind \
  | jq -r '.[0].IPAM.Config[] | select(.Subnet | test("^[0-9.]+/[0-9]+$")) | .Subnet' | head -1)
prefix=$(echo "$subnet" | cut -d. -f1-3)
ips="${prefix}.100-${prefix}.110"

# --- Install ----------------------------------------------------------------
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml \
  || fail "metallb manifest apply failed"

# Make sure nodes and CoreDNS are up before anything depends on them
kubectl wait --for=condition=Ready nodes --all --timeout=${WAIT}s || fail "nodes not ready"
kubectl -n kube-system rollout status deploy/coredns --timeout=${WAIT}s || fail "coredns not ready"

kubectl -n $NS rollout status deploy/controller --timeout=${WAIT}s || fail "controller not ready"
kubectl -n $NS rollout status ds/speaker        --timeout=${WAIT}s || fail "speaker not ready"

# Webhook needs a ready endpoint before the pool can be applied
kubectl -n $NS wait --for=jsonpath='{.subsets[0].addresses[0].ip}' \
  endpoints/metallb-webhook-service --timeout=${WAIT}s || fail "webhook has no endpoints"

# --- Pool + L2 advertisement (retry until the webhook accepts it) ------------
apply_pool() {
  kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata: {name: default-address-pool, namespace: $NS}
spec: {addresses: ["$ips"]}
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata: {name: default-l2, namespace: $NS}
spec: {ipAddressPools: [default-address-pool]}
EOF
}
ok=0
for ((i=0; i<WAIT/5; i++)); do apply_pool && { ok=1; break; }; sleep 5; done
[[ $ok == 1 ]] || fail "IPAddressPool never accepted"

# excludel2 ConfigMap: keep your existing block here unchanged

# --- nginx test (strict) -----------------------------------------------------
kubectl create deployment nginx --image=nginx:alpine --dry-run=client -o yaml | kubectl apply -f -
kubectl expose deployment nginx --port=80 --type=LoadBalancer --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout status deploy/nginx --timeout=${WAIT}s || fail "nginx not available"

# LB IP
ip=""
for ((i=0; i<WAIT/5; i++)); do
  ip=$(kubectl get svc nginx -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
  [[ -n "$ip" ]] && break
  sleep 5
done
[[ -n "$ip" ]] || fail "no LoadBalancer IP assigned"
echo "LoadBalancer IP acquired: $ip"

wait_for_endpoints default nginx ${WAIT}

# One long-lived client pod instead of a new pod per attempt
kubectl run curl-client --image=curlimages/curl --restart=Never --command -- sleep 3600
kubectl wait --for=condition=Ready pod/curl-client --timeout=${WAIT}s || fail "curl-client not ready"

probe() {
  kubectl exec curl-client -- \
    curl -sS --fail --connect-timeout 3 --max-time 5 "$1" 2>&1 | grep -q "Welcome to nginx"
}

check() { # check <label> <url>
  for ((i=0; i<WAIT/5; i++)); do
    if probe "$2"; then echo "✅ $1 reachable ($2)"; return 0; fi
    echo "$1 not reachable yet ($2), retrying in 5s... ($((i*5))/${WAIT}s)"
    sleep 5
  done
  return 1
}

clusterip=$(kubectl get svc nginx -o jsonpath='{.spec.clusterIP}')
check "ClusterIP" "http://$clusterip"                    || fail "ClusterIP unreachable (kube-proxy/CNI service rules?)"
check "DNS"       "http://nginx.default.svc.cluster.local" || fail "service DNS unreachable"
check "LB IP"     "http://$ip"                           || fail "LoadBalancer IP unreachable"

kubectl delete pod/curl-client svc/nginx deploy/nginx --ignore-not-found --wait=false
echo "MetalLB ready and verified."