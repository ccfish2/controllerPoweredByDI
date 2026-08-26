#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
#source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

# Install the sample application and the Cilium resources required by the
# Gateway implementation before creating any Gateway API objects.
kubectl -n "${NAMESPACE}" apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml
echo "Deploy Cilium CRDS"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive || true # cilium CRDs
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-agent.yaml
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-envoy.yaml

echo "Checking kube-system cilium agent and envoy pods are ready"
NAMESPACE="kube-system"
TIMEOUT=120
INTERVAL=5
wait_for_pods "k8s-app=cilium" "cilium agent" || exit 1
wait_for_pods "k8s-app=cilium-envoy" "cilium-envoy" || exit 1
NAMESPACE="dolphin"

# Remove the legacy static CEC so it cannot reconcile a Service that this test
# does not create. The active passthrough CEC is applied below.
kubectl -n "${NAMESPACE}" delete ciliumenvoyconfig cilium-gateway-my-gateway \
  --ignore-not-found=true

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: ${GATEWAY_CLASS}
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF

# The GatewayClass must be accepted before listeners and routes can become
# ready; the helper polls its status with a bounded timeout.
wait_for_gatewayclass_accepted "${GATEWAY_CLASS}" 120 5

#!/usr/bin/env bash
set -euo pipefail

# gRPC Gateway Controller API test, following Cilium's documented pattern:
# https://docs.cilium.io/en/v1.19/network/servicemesh/gateway-api/grpc/
#
# we use openssl instead of mkcert which is more practical in daily work

#!/usr/bin/env bash
set -euo pipefail

# gRPC Gateway API test, following Cilium's documented pattern:
# https://docs.cilium.io/en/v1.19/network/servicemesh/gateway-api/grpc/
#
# Mirrors the structure of the TLS-passthrough test script: cert generation,
# resource apply, poll-until-ready loops, and a dump_debug helper for failures.

echo "Generating TLS certificate"

DOMAIN="grpc-echo.cilium.rocks"
NAMESPACE="${NAMESPACE:-default}"
GATEWAY_CLASS="${GATEWAY_CLASS:-cilium}"
CERT_DIR="$(mktemp -d)"
trap 'rm -rf "${CERT_DIR}"' EXIT

GATEWAY_NAME="tls-gateway"
ROUTE_NAME="grpc-route"
SERVICE_NAME="grpc-echo"
SERVICE_PORT=7070

openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "${CERT_DIR}/tls.key" \
  -out "${CERT_DIR}/tls.crt" \
  -days 1 \
  -subj "/CN=${DOMAIN}" \
  -addext "subjectAltName=DNS:${DOMAIN}"

kubectl create namespace "${NAMESPACE}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${NAMESPACE}" create secret tls grpc-certificate \
  --cert="${CERT_DIR}/tls.crt" \
  --key="${CERT_DIR}/tls.key" \
  --dry-run=client -o yaml |
  kubectl apply -f -

echo "Deploying gRPC echo backend"

# grpc-echo.cilium.rocks is Cilium's reference gRPC echo server image used
# throughout their docs/e2e suite. Swap for your own controller's image if
# you're testing something other than the reference backend.
kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${SERVICE_NAME}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: ${SERVICE_NAME}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ${SERVICE_NAME}
    spec:
      containers:
      - name: grpc-echo
        image: kong/go-echo:0.6.0
        ports:
        - name: grpc
          containerPort: ${SERVICE_PORT}
---
apiVersion: v1
kind: Service
metadata:
  name: ${SERVICE_NAME}
spec:
  selector:
    app.kubernetes.io/name: ${SERVICE_NAME}
  ports:
  - name: grpc
    port: ${SERVICE_PORT}
    targetPort: ${SERVICE_PORT}
    # Required so the route uses plaintext HTTP/2 to the backend instead of
    # attempting HTTP/1.1 — without this you'll see protocol errors.
    appProtocol: kubernetes.io/h2c
EOF

kubectl -n "${NAMESPACE}" rollout status \
  deployment/"${SERVICE_NAME}" --timeout=120s

echo "Creating Gateway and GRPCRoute"

kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${GATEWAY_NAME}
spec:
  gatewayClassName: ${GATEWAY_CLASS}
  listeners:
  - name: https
    protocol: HTTPS
    port: 443
    hostname: ${DOMAIN}
    tls:
      certificateRefs:
      - kind: Secret
        name: grpc-certificate
---
apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: ${ROUTE_NAME}
spec:
  parentRefs:
  - name: ${GATEWAY_NAME}
  hostnames:
  - ${DOMAIN}
  rules:
  - backendRefs:
    - name: ${SERVICE_NAME}
      port: ${SERVICE_PORT}
EOF

dump_debug() {
  echo "=== GRPCRoute diagnostics ==="
  kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get grpcroute "${ROUTE_NAME}" -o yaml || true
  kubectl -n "${NAMESPACE}" get svc "${SERVICE_NAME}" -o wide || true
  kubectl -n "${NAMESPACE}" get endpoints "${SERVICE_NAME}" -o yaml || true
  kubectl get pods -A -l k8s-app=cilium-envoy -o wide || true
  kubectl -n "${NAMESPACE}" get pods -o wide || true
  kubectl get nodes -o wide || true
  kubectl get events -A --sort-by='.lastTimestamp' | tail -80 || true
  echo "=== End GRPCRoute diagnostics ==="
}

echo "Deploy GRPC route cilium envoy config"
kubectl -n dolphin apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/grpc-cec.yaml
sleep 10

echo "Preparing netshoot client"

# Reuse the same pattern as the TLS passthrough test: a disposable in-cluster
# client with the CA certificate mounted directly from the Secret, and
# grpcurl installed alongside curl/openssl in the netshoot image.
kubectl -n "${NAMESPACE}" delete pod netshoot --ignore-not-found --wait=true
kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  labels:
    run: netshoot
  name: netshoot
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
      secretName: grpc-certificate
      items:
      - key: tls.crt
        path: ${DOMAIN}.pem
EOF

kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/netshoot --timeout=120s

# netshoot doesn't ship grpcurl by default; install it once into the running
# container so subsequent kubectl exec calls can just invoke it directly.
if ! kubectl -n "${NAMESPACE}" exec netshoot -- which grpcurl >/dev/null 2>&1; then
  echo "Installing grpcurl into netshoot"
  GRPCURL_ARCH="$(kubectl -n "${NAMESPACE}" exec netshoot -- uname -m)"
  case "${GRPCURL_ARCH}" in
    x86_64) GRPCURL_ARCH="amd64" ;;
    aarch64) GRPCURL_ARCH="arm64" ;;
  esac
  GRPCURL_VERSION="$(
    curl -sSL https://api.github.com/repos/fullstorydev/grpcurl/releases/latest |
      grep -oP '"tag_name": "v\K[^"]+'
  )"
  kubectl -n "${NAMESPACE}" exec netshoot -- sh -c "
    curl -sSLf https://github.com/fullstorydev/grpcurl/releases/download/v${GRPCURL_VERSION}/grpcurl_${GRPCURL_VERSION}_linux_${GRPCURL_ARCH}.tar.gz \
      -o /tmp/grpcurl.tar.gz &&
    tar -xzf /tmp/grpcurl.tar.gz -C /usr/local/bin grpcurl &&
    rm /tmp/grpcurl.tar.gz
  "
fi

echo "Waiting for Gateway to be programmed and GRPCRoute to be accepted"

deadline=$((SECONDS + 120))
gateway_ip=""

while (( SECONDS < deadline )); do
  gateway_programmed="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' \
      2>/dev/null || true
  )"

  route_accepted="$(
    kubectl -n "${NAMESPACE}" get grpcroute "${ROUTE_NAME}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' \
      2>/dev/null || true
  )"

  route_refs_resolved="$(
    kubectl -n "${NAMESPACE}" get grpcroute "${ROUTE_NAME}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}' \
      2>/dev/null || true
  )"

  gateway_ip="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.addresses[?(@.type=="IPAddress")].value}' \
      2>/dev/null || true
  )"

  if [[ "${gateway_programmed}" == "True" &&
    "${route_accepted}" == "True" &&
    "${route_refs_resolved}" == "True" &&
    -n "${gateway_ip}" ]]; then
    break
  fi

  sleep 5
done

if [[ "${gateway_programmed:-}" != "True" ||
  "${route_accepted:-}" != "True" ||
  "${route_refs_resolved:-}" != "True" ||
  -z "${gateway_ip}" ]]; then
  echo "Gateway/GRPCRoute did not become ready in time"
  dump_debug
  exit 1
fi

echo "Gateway address: ${gateway_ip}"

echo "Testing gRPC call through ${gateway_ip}:443 (from inside netshoot)"

# grpcurl runs inside the netshoot pod, in-cluster, talking directly to the
# Gateway IP but sending the configured hostname as :authority (the gRPC
# equivalent of --connect-to + SNI in the curl passthrough test). It verifies
# the cert we generated above, mounted from the Secret rather than a
# separately serialized copy.
response="$(kubectl -n "${NAMESPACE}" exec netshoot -- grpcurl \
  -cacert "/certs/${DOMAIN}.pem" \
  -authority "${DOMAIN}" \
  "${gateway_ip}:443" \
  proto.EchoTestService/Echo)" || {
  echo "gRPC request through Gateway failed"
  dump_debug
  exit 1
}

echo "gRPC response:"
echo "${response}"

if ! grep -q "StatusCode=200" <<<"${response}"; then
  echo "Unexpected gRPC response (missing StatusCode=200)"
  dump_debug
  exit 1
fi

echo "GRPCRoute test passed"