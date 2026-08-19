#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

kubectl -n "${NAMESPACE}" apply -f .github/applications-for-conformance/books.info

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

echo "Generating TLS certificate"

DOMAIN="bookinfo.cilium.rocks"
CERT_DIR="$(mktemp -d)"
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

echo "Deploying TLS-terminating Gateway and HTTPRoute"

kubectl apply -f \
  "${SCRIPT_DIR}/ingressintegrationtests_setup/gatewayapi/gatewayhttproute.yaml"
check_service_external_ip "dolphin-gateway-tls-gateway"

kubectl apply -f \
  "${SCRIPT_DIR}/ingressintegrationtests_setup/gatewayapi/envoyconfig.yaml"

wait_for_httproute_ready "${NAMESPACE}" "https-app-route-1" 120 5
verify_gateway_ready "${NAMESPACE}" "tls-gateway" 120 5
verify_gateway_tls_listener_ready "${NAMESPACE}" "tls-gateway" "https-1" 120 5

echo "TLS termination test passed"

echo "Deploying TLS passthrough backend"

PASSTHROUGH_DOMAIN="passthrough.bookinfo.cilium.rocks"
PASSTHROUGH_GATEWAY="tls-passthrough-gateway"
PASSTHROUGH_ROUTE="tls-passthrough-route"
PASSTHROUGH_SERVICE="tls-passthrough-backend"

openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "${CERT_DIR}/passthrough.key" \
  -out "${CERT_DIR}/passthrough.crt" \
  -days 1 \
  -subj "/CN=${PASSTHROUGH_DOMAIN}" \
  -addext "subjectAltName=DNS:${PASSTHROUGH_DOMAIN}"

kubectl -n "${NAMESPACE}" create secret tls passthrough-backend-tls \
  --cert="${CERT_DIR}/passthrough.crt" \
  --key="${CERT_DIR}/passthrough.key" \
  --dry-run=client -o yaml |
  kubectl apply -f -

kubectl -n "${NAMESPACE}" apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: passthrough-backend-config
data:
  default.conf: |
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
---
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

echo "Testing TLS passthrough through ${gateway_ip}:443"

response="$(
  curl --silent --show-error --insecure \
    --resolve "${PASSTHROUGH_DOMAIN}:443:${gateway_ip}" \
    "https://${PASSTHROUGH_DOMAIN}/"
)"

if [[ "${response}" != "TLS passthrough backend" ]]; then
  echo "Unexpected backend response: ${response}"
  exit 1
fi

certificate="$(
  openssl s_client \
    -connect "${gateway_ip}:443" \
    -servername "${PASSTHROUGH_DOMAIN}" \
    </dev/null 2>/dev/null |
    openssl x509 -noout -subject
)"

if ! grep -q "CN = ${PASSTHROUGH_DOMAIN}" <<<"${certificate}"; then
  echo "Backend certificate was not returned through the Gateway:"
  echo "${certificate}"
  exit 1
fi

echo "TLS passthrough test passed"