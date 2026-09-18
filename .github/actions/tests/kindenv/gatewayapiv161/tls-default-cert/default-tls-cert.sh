#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

#!/usr/bin/env bash
set -euo pipefail

echo "Install/upgrade Cilium Agent and Envoy"

NAMESPACE="kube-system"
CILIUM_VERSION="1.20.1"
TIMEOUT=120
INTERVAL=5

# Add repo if needed; update is safe to run repeatedly.
helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
helm repo update >/dev/null

# Install on first run, upgrade on subsequent runs.
helm upgrade --install cilium cilium/cilium \
    --version "${CILIUM_VERSION}" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    --set kubeProxyReplacement=true \
    --set gatewayAPI.enabled=true \
    --set operator.replicas=0

echo "Waiting for Cilium agent pods to be ready..."
wait_for_pods "k8s-app=cilium" "cilium agent" || exit 1

echo "Waiting for Cilium Envoy pods to be ready..."
wait_for_pods "k8s-app=cilium-envoy" "cilium-envoy" || exit 1

echo "Ensuring Cilium Operator is not running..."

# In case an older installation created the operator, remove it.
if kubectl -n "${NAMESPACE}" get deployment cilium-operator >/dev/null 2>&1; then
    kubectl -n "${NAMESPACE}" delete deployment cilium-operator --ignore-not-found
fi

echo "Waiting for Cilium Operator deployment to disappear..."
for ((elapsed=0; elapsed<TIMEOUT; elapsed+=INTERVAL)); do
    if ! kubectl -n "${NAMESPACE}" get deployment cilium-operator >/dev/null 2>&1; then
        echo "Cilium Operator is absent."
        break
    fi

    sleep "${INTERVAL}"
done

echo "Cilium installation complete."

kubectl -n "${NAMESPACE}" get pods -l k8s-app=cilium -o wide
kubectl -n "${NAMESPACE}" get pods -l k8s-app=cilium-envoy -o wide

# Install the sample application and the Cilium resources required by the
# Gateway implementation before creating any Gateway API objects.
NAMESPACE=dolphin
#kubectl -n "${NAMESPACE}" apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml
kubectl -n "${NAMESPACE}" apply -f .github/applications-for-conformance/books-info.yaml
wait_for_endpoints dolphin details || exit 1
wait_for_endpoints dolphin productpage || exit 1

echo "Deploying gatewayclass and gateway"
NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: ${GATEWAY_CLASS}
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF


DOMAIN="bookinfo.cilium.rocks"
CERT_FILE="${DOMAIN}.pem"
KEY_FILE="${DOMAIN}-key.pem"
 
# --- Generate cert ---
if [[ ! -d mkcert ]]; then
  git clone https://github.com/FiloSottile/mkcert.git
fi
cd mkcert
go build -ldflags "-X main.Version=$(git describe --tags)"
ls -l mkcert
if [[ ! -x ./mkcert ]]; then
  echo "mkcert binary does not exits"
  ls -l
  exit 1
fi

echo "binary mkcert exist"
ls -l mkcert
./mkcert $DOMAIN
 
# --- Push cert material into cilium-secrets so Cilium's SDS watcher (envoy-secrets-namespace) picks it up ---
kubectl create namespace cilium-secrets --dry-run=client -o yaml | kubectl apply -f -
 
kubectl -n cilium-secrets delete secret tls-default-secret --ignore-not-found

kubectl -n cilium-secrets create secret tls tls-default-secret \
  --cert=$CERT_FILE \
  --key=$KEY_FILE

kubectl -n dolphin create secret tls tls-default-secret \
  --cert=$CERT_FILE \
  --key=$KEY_FILE

echo "Deploying TCP Route"
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: tls-gateway
  namespace: ${NAMESPACE}
spec:
  gatewayClassName: dolphin
  listeners:
  - name: default
    protocol: HTTPS
    port: 443
    tls:
      certificateRefs:
      - kind: Secret
        name: demo-cert
      mode: Terminate
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: bookinfo
  namespace: ${NAMESPACE}
spec:
  parentRefs:
  - name: tls-gateway
    sectionName: default
  hostnames:
  - "bookinfo.cilium.rocks"
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    backendRefs:
    - name: details
      port: 9080
EOF


kubectl -n dolphin delete pod busybox --ignore-not-found --wait=true
echo "Taking a look at the pods status on the GH Actions cluster"

kubectl -n dolphin get pods -o wide || true
kubectl -n dolphin get pods -l app=details -o yaml 2>/dev/null \
  | grep -A5 -E "phase|reason|message" || true
kubectl -n dolphin describe pod -l app=details || true
kubectl get events -n dolphin --sort-by='.lastTimestamp' | tail -40 || true
kubectl top nodes 2>/dev/null || echo "metrics-server not installed"
kubectl describe nodes | grep -A5 -E "Conditions:|Allocated resources" || true

set -uo pipefail

NAMESPACE="dolphin"
POD="netshoot"
HOST="bookinfo.cilium.rocks"
CACERT="/certs/${HOST}.pem"
URL="https://${HOST}/details/1"

echo "NAMESPACE=${NAMESPACE}"
echo "POD=${POD}"
echo "HOST=${HOST}"
echo "CACERT=${CACERT}"
echo "URL=${URL}"

kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  labels:
    run: netshoot
  name: netshoot
  namespace: dolphin
spec:
  containers:
  - command:
    - sleep
    - infinity
    image: nicolaka/netshoot
    name: netshoot
    volumeMounts:
    - name: ca-cert
      mountPath: /certs
  volumes:
  - name: ca-cert
    configMap:
      name: bookinfo-ca
EOF

kubectl -n "${NAMESPACE}" wait \
  --for=condition=Ready \
  pod/"${POD}" \
  --timeout=60s

WAIT_STATUS=$?

if [[ $WAIT_STATUS -ne 0 ]]; then
  echo "ERROR: netshoot verification pod did not reach Succeeded phase"
  kubectl -n "${NAMESPACE}" describe pod "${POD}" || true
  kubectl -n "${NAMESPACE}" logs "${POD}" || true
  exit 1
fi

echo "Resolving ${HOST} -> ${tlsingressip}"
if [[ -z "${tlsingressip:-}" ]]; then
    echo "ERROR: tlsingressip is not set"
    exit 1
fi

set -x

echo "Running:"
printf 'kubectl -n "%s" exec "%s" -- curl -sSL -o /tmp/response.json -w "%%{http_code}" --resolve "%s:443:%s" --cacert "%s" "%s"\n' \
    "${NAMESPACE}" \
    "${POD}" \
    "${HOST}" \
    "${tlsingressip}" \
    "${CACERT}" \
    "${URL}"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:443:${tlsingressip}" \
  --cacert "${CACERT}" \
  "${URL}"; then
    echo "TLS ingress verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi