#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

# Install the sample application and the Cilium resources required by the
# Gateway implementation before creating any Gateway API objects.
kubectl -n "${NAMESPACE}" apply -f /Users/jiminhu/Documents/github.com/controllerPoweredByDI/.github/applications-for-conformance/books-info.yaml
echo "Deploy Cilium CRDS"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive # cilium CRDs
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

echo "Generating TLS certificate"

DOMAIN="bookinfo.cilium.rocks"
CERT_DIR="$(mktemp -d)"
# Keep the short-lived test certificates out of the repository and remove
# them automatically when the script exits, including on failure.
trap 'rm -rf "${CERT_DIR}"' EXIT

openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "${CERT_DIR}/tls.key" \
  -out "${CERT_DIR}/tls.crt" \
  -days 1 \
  -subj "/CN=${DOMAIN}" \
  -addext "subjectAltName=DNS:${DOMAIN}"

for namespace in "${NAMESPACE}" cilium-secrets; do
  kubectl create namespace "${namespace}" \
    --dry-run=client -o yaml | kubectl apply -f -

  kubectl -n "${namespace}" create secret tls tls-ingress-secret \
    --cert="${CERT_DIR}/tls.crt" \
    --key="${CERT_DIR}/tls.key" \
    --dry-run=client -o yaml |
    kubectl apply -f -
done

echo "Deploying TLS passthrough backend"

PASSTHROUGH_DOMAIN="passthrough.bookinfo.cilium.rocks"
PASSTHROUGH_GATEWAY="tls-passthrough-gateway"
PASSTHROUGH_ROUTE="tls-passthrough-route"
PASSTHROUGH_SERVICE="tls-passthrough-backend"

openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "${CERT_DIR}/passthrough.key" \
  -out "${CERT_DIR}/passthrough.crt" \
  -days 2 \
  -subj "/CN=${PASSTHROUGH_DOMAIN}" \
  -addext "subjectAltName=DNS:${PASSTHROUGH_DOMAIN}"

kubectl -n "${NAMESPACE}" create secret tls passthrough-backend-tls \
  --cert="${CERT_DIR}/passthrough.crt" \
  --key="${CERT_DIR}/passthrough.key" \
  --dry-run=client -o yaml |
  kubectl apply -f -

PASSTHROUGH_CERT_HASH="$(sha256sum "${CERT_DIR}/passthrough.crt" | awk '{print $1}')"

# Keep the Nginx configuration in a ConfigMap. The netshoot client mounts the
# certificate directly from the Secret used by the backend below.
cat >"${CERT_DIR}/default.conf" <<EOF
server {
  listen 8443 ssl;
  server_name ${PASSTHROUGH_DOMAIN};

  ssl_certificate /etc/tls/tls.crt;
  ssl_certificate_key /etc/tls/tls.key;

  location / {
    default_type text/plain;
    return 200 "TLS passthrough backend\n";
  }
}
EOF

kubectl -n "${NAMESPACE}" create configmap passthrough-backend-config \
  --from-file=default.conf="${CERT_DIR}/default.conf" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${PASSTHROUGH_SERVICE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${PASSTHROUGH_SERVICE}
  template:
    metadata:
      labels:
        app: ${PASSTHROUGH_SERVICE}
      annotations:
        dolphin.io/tls-certificate-hash: ${PASSTHROUGH_CERT_HASH}
    spec:
      containers:
      - name: nginx
        image: nginx:1.27-alpine
        ports:
        - name: tls
          containerPort: 8443
        volumeMounts:
        - name: config
          mountPath: /etc/nginx/conf.d
        - name: tls
          mountPath: /etc/tls
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: passthrough-backend-config
      - name: tls
        secret:
          secretName: passthrough-backend-tls
---
apiVersion: v1
kind: Service
metadata:
  name: ${PASSTHROUGH_SERVICE}
spec:
  selector:
    app: ${PASSTHROUGH_SERVICE}
  ports:
  - name: tls
    port: 443
    targetPort: 8443
EOF

kubectl -n "${NAMESPACE}" rollout status \
  deployment/"${PASSTHROUGH_SERVICE}" --timeout=120s

# Create a disposable in-cluster client with the exact backend certificate
# mounted from the Secret. This avoids trusting a separately serialized copy.
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
      secretName: passthrough-backend-tls
      items:
      - key: tls.crt
        path: ${PASSTHROUGH_DOMAIN}.pem
EOF

kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/netshoot --timeout=120s

# Bind the TLSRoute to a TLS listener and route traffic to the HTTPS backend.
# Please be noted that all TLS backends share the same TLS port 443 from the LoadBalancer
# Envoy - either CNI controller or your customized controller would inspect the SNI through 
# the TLS handshake and thus forward the TLS stream to the backend service 
kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${PASSTHROUGH_GATEWAY}
spec:
  gatewayClassName: ${GATEWAY_CLASS}
  listeners:
  - name: tls-passthrough
    protocol: TLS
    port: 443
    hostname: ${PASSTHROUGH_DOMAIN}
    tls:
      mode: Passthrough
    allowedRoutes:
      namespaces:
        from: Same
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: ${PASSTHROUGH_ROUTE}
spec:
  parentRefs:
  - name: ${PASSTHROUGH_GATEWAY}
    sectionName: tls-passthrough
  hostnames:
  - ${PASSTHROUGH_DOMAIN}
  rules:
  - backendRefs:
    - name: ${PASSTHROUGH_SERVICE}
      port: 443
EOF

echo "Waiting for TLSRoute and Gateway"

# Wait until the route has an address, then wait for the controller to report
# that the Gateway is programmed and the route references resolved.
deadline=$((SECONDS + 120))
gateway_ip=""

while (( SECONDS < deadline )); do
  route_accepted="$(
    kubectl -n "${NAMESPACE}" get tlsroute "${PASSTHROUGH_ROUTE}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' \
      2>/dev/null || true
  )"

  gateway_ip="$(
    kubectl -n "${NAMESPACE}" get gateway "${PASSTHROUGH_GATEWAY}" \
      -o jsonpath='{.status.addresses[?(@.type=="IPAddress")].value}' \
      2>/dev/null || true
  )"

  if [[ "${route_accepted}" == "True" && -n "${gateway_ip}" ]]; then
    break
  fi

  sleep 5
done

if [[ -z "${gateway_ip}" ]]; then
  kubectl -n "${NAMESPACE}" get gateway,tlsroute,svc
  exit 1
fi

# Use the address assigned to this passthrough Gateway instead of hard-coding
# a Kind/MetalLB IP, since the address can change between test runs.
gateway_ip="$(kubectl -n "${NAMESPACE}" get gateway "${PASSTHROUGH_GATEWAY}" \
  -o jsonpath='{.status.addresses[?(@.type=="IPAddress")].value}')"

echo "Testing TLS passthrough through ${gateway_ip}:443"

dump_passthrough_debug() {
  echo "=== TLS passthrough diagnostics ==="
  echo "Gateway address: ${gateway_ip:-<unset>}"
  kubectl -n "${NAMESPACE}" get gateway "${PASSTHROUGH_GATEWAY}" -o yaml || true
  kubectl -n "${NAMESPACE}" get tlsroute "${PASSTHROUGH_ROUTE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get svc "${PASSTHROUGH_SERVICE}" -o wide || true
  kubectl -n "${NAMESPACE}" get endpoints "${PASSTHROUGH_SERVICE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get endpointslice \
    -l "kubernetes.io/service-name=${PASSTHROUGH_SERVICE}" -o yaml || true
  kubectl -n "${NAMESPACE}" get ciliumenvoyconfig \
    "dolphin-gateway-${PASSTHROUGH_GATEWAY}" -o yaml || true
  kubectl get pods -A -l k8s-app=cilium-envoy -o wide || true
  kubectl -n "${NAMESPACE}" get pods -o wide || true
  kubectl get nodes -o wide || true
  kubectl get events -A --sort-by='.lastTimestamp' | tail -80 || true
  echo "=== End TLS passthrough diagnostics ==="
}

echo "Gateway status:"
kubectl -n "${NAMESPACE}" get gateway "${PASSTHROUGH_GATEWAY}" -o yaml

echo "TLSRoute status:"
kubectl -n "${NAMESPACE}" get tlsroute "${PASSTHROUGH_ROUTE}" -o yaml

echo "Passthrough service:"
kubectl -n "${NAMESPACE}" get svc "${PASSTHROUGH_SERVICE}" -o wide
kubectl -n "${NAMESPACE}" get endpoints "${PASSTHROUGH_SERVICE}" -o yaml

# Install the Envoy path configuration used to connect the external Gateway
# address to the backend pods through Cilium's datapath.
kubectl -n "${NAMESPACE}" apply -f \
  "${SCRIPT_DIR}/ingressintegrationtests_setup/gatewayapi/tls-paththrough/ciliumenvoconfig.yaml"
sleep 10

dump_passthrough_debug

deadline=$((SECONDS + 120))
while (( SECONDS < deadline )); do
  gateway_programmed="$(
    kubectl -n "${NAMESPACE}" get gateway "${PASSTHROUGH_GATEWAY}" \
      -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' \
      2>/dev/null || true
  )"

  route_accepted="$(
    kubectl -n "${NAMESPACE}" get tlsroute "${PASSTHROUGH_ROUTE}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' \
      2>/dev/null || true
  )"

  route_refs_resolved="$(
    kubectl -n "${NAMESPACE}" get tlsroute "${PASSTHROUGH_ROUTE}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}' \
      2>/dev/null || true
  )"

  if [[ "${gateway_programmed}" == "True" &&
    "${route_accepted}" == "True" &&
    "${route_refs_resolved}" == "True" ]]; then
    break
  fi

  sleep 5
done

if [[ "${gateway_programmed:-}" != "True" ||
  "${route_accepted:-}" != "True" ||
  "${route_refs_resolved:-}" != "True" ]]; then
  kubectl -n "${NAMESPACE}" get gateway,tlsroute,svc
  exit 1
fi

# Check the listener before making an HTTPS request so a missing datapath path
# produces a focused failure instead of a less useful curl timeout.
# echo "Checking TCP connectivity to ${gateway_ip}:443"
# if ! nc -vz -w 5 "${gateway_ip}" 443; then
#   echo "Cannot connect to ${gateway_ip}:443"
#   exit 1
# fi

# --connect-to directs the connection to the Gateway IP while keeping the
# requested hostname (and therefore SNI). The mounted Secret certificate lets
# curl verify the self-signed certificate returned by the backend.
# we use the SNI connect to the backend 
response="$(kubectl -n "${NAMESPACE}" exec netshoot -- curl \
  --verbose \
  --trace-time \
  --noproxy '*' \
  --connect-timeout 10 \
  --max-time 30 \
  --retry 10 \
  --retry-all-errors \
  --retry-delay 2 \
  --retry-max-time 120 \
  --silent \
  --show-error \
  --cacert "/certs/${PASSTHROUGH_DOMAIN}.pem" \
  --connect-to "${PASSTHROUGH_DOMAIN}:443:${gateway_ip}:443" \
  "https://${PASSTHROUGH_DOMAIN}/details/v1")" || {
  echo "TLS passthrough curl request failed"
  dump_passthrough_debug
  exit 1
}

echo "TLS passthrough curl succeeded"

# Inspect the certificate for diagnostics. Curl already verifies the
# certificate and hostname above, so differences in openssl output must not
# make the functional test fail.
echo "Inspecting backend certificate"
certificate="$(
  kubectl -n "${NAMESPACE}" exec netshoot -- openssl s_client \
    -connect "${gateway_ip}:443" \
    -servername "${PASSTHROUGH_DOMAIN}" \
    -connect_timeout 10 \
    -brief \
    </dev/null 2>&1
  )" || true

echo "TLS handshake output:"
echo "${certificate}"
if ! grep -q "subject=.*CN = ${PASSTHROUGH_DOMAIN}" <<<"${certificate}"; then
  echo "Warning: backend certificate subject was not found in openssl output"
fi

# expected response would be
# 00:29:45.619327 < Connection: keep-alive 00:29:45.619337 <  TLS passthrough backend
printf 'Backend response: %q\n' "${response}"

if [[ "${response}" != "TLS passthrough backend" ]]; then
  echo "Unexpected backend response"
  exit 1
fi

echo "TLS passthrough test passed"
