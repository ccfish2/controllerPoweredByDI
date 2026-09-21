#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

#!/usr/bin/env bash
#!/usr/bin/env bash
set -euo pipefail

echo "Install/upgrade Cilium Agent and Envoy"

NAMESPACE="kube-system"
CILIUM_VERSION="1.20.1"
TIMEOUT=300
INTERVAL=5

echo "Installing/upgrading Cilium ${CILIUM_VERSION}..."
helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
helm repo update >/dev/null

CILIUM_HELM_TIMEOUT=300
if ! helm upgrade --install cilium cilium/cilium \
    --version "${CILIUM_VERSION}" \
    --namespace "${NAMESPACE}" \
    --create-namespace \
    --set kubeProxyReplacement=true \
    --set gatewayAPI.enabled=true \
    --set operator.replicas=0 \
    --wait \
    --timeout "${CILIUM_HELM_TIMEOUT}s"
then
    echo "Cilium failed to become ready; collecting diagnostics..."

    kubectl -n kube-system get pods -o wide || true

    echo "----- Cilium DaemonSet -----"
    kubectl -n kube-system get ds cilium -o wide || true
    kubectl -n kube-system describe ds cilium || true

    echo "----- Cilium pod descriptions -----"
    kubectl -n kube-system describe pods \
        -l k8s-app=cilium || true

    echo "----- Cilium logs -----"
    kubectl -n kube-system logs \
        -l k8s-app=cilium \
        --all-containers=true \
        --tail=300 \
        --prefix || true

    echo "----- kube-system events -----"
    kubectl get events -n kube-system \
        --sort-by='.lastTimestamp' | tail -100 || true

    echo "----- all nodes -----"
    kubectl get nodes -o wide || true

    echo "----- node conditions/resources -----"
    kubectl describe nodes | \
        grep -A10 -E "Conditions:|Allocated resources" || true

    echo "----- Helm status -----"
    helm status cilium -n kube-system || true
    exit 1
fi

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

kubectl -n "${NAMESPACE}" get pods -l k8s-app=cilium -o wide
kubectl -n "${NAMESPACE}" get pods -l k8s-app=cilium-envoy -o wide

echo "Cilium installation complete."

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

# Install the sample application and the Cilium resources required by the
echo "Install the sample application and the Cilium Resources"
# Gateway implementation before creating any Gateway API objects.
kubectl -n "${NAMESPACE}" apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml
wait_for_endpoints dolphin details || exit 1
wait_for_endpoints dolphin productpage || exit 1
echo "Deploying gatewayclass and gateway"
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: ${GATEWAY_CLASS}
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF

wait_for_gatewayclass_accepted "${GATEWAY_CLASS}" 120 5

echo "Install Certs using mkcert and generate tls secrets using the cert"
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
# for cilium
kubectl -n cilium-secrets delete secret tls-default-secret --ignore-not-found
kubectl -n cilium-secrets create secret tls tls-default-secret \
  --cert=$CERT_FILE \
  --key=$KEY_FILE
# for netshoot pod
kubectl -n dolphin delete secret tls-default-secret --ignore-not-found
kubectl -n dolphin create secret tls tls-default-secret \
  --cert=$CERT_FILE \
  --key=$KEY_FILE

echo "Deploying Gateway and HTTP Route"
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
        name: tls-default-secret
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

echo "Taking a look at the pods status on the GH Actions cluster"
kubectl -n dolphin get pods -o wide || true
kubectl -n dolphin get pods -l app=details -o yaml 2>/dev/null \
  | grep -A5 -E "phase|reason|message" || true
kubectl -n dolphin describe pod -l app=details || true
kubectl get events -n dolphin --sort-by='.lastTimestamp' | tail -40 || true
kubectl top nodes 2>/dev/null || echo "metrics-server not installed"
kubectl describe nodes | grep -A5 -E "Conditions:|Allocated resources" || true

# verify gatewayapi through l7 service connection
gatewayip=""
end=$((SECONDS+120))
while true; do
    gatewayip=$(kubectl -n dolphin get gateway tls-gateway -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

    if [[ -n "$gatewayip" ]]; then
        echo "Gateway Service IP acquired: $gatewayip"
        break
    fi

    echo "Waiting for Gateway IP..."
    sleep 5

    if ((SECONDS > end)); then
        echo "Timeout waiting for Gateway Service IP"
        exit 1
    fi
done

echo "Deploying cilium envoy config"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tls-default-cert/cec-tls-gw.yaml

set -uo pipefail
echo "deploying debug pod on the samenamespace mounted with the same secrets"
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
    secret:
      secretName: tls-default-secret
      items:
      - key: tls.crt
        path: bookinfo.cilium.rocks.pem
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
# set -x

echo "Running Test:"
printf 'kubectl -n "%s" exec "%s" -- curl -sSL -o /tmp/response.json -w "%%{http_code}" --resolve "%s:443:%s" --cacert "%s" "%s"\n' \
    "${NAMESPACE}" \
    "${POD}" \
    "${HOST}" \
    "${gatewayip}" \
    "${CACERT}" \
    "${URL}"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --connect-to "${HOST}:443:${gatewayip}" \
  --cacert "${CACERT}" \
  "${URL}"; then
    echo "Default TLS verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi
#
#curl --verbose --trace-time --show-error --cacert /certs/bookinfo.cilium.rocks.pem --connect-to "bookinfo.cilium.rocks.pem:443:172.18.0.101:443" https://bookinfo.cilium.rocks/details/v1