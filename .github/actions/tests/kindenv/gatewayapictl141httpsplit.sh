#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

echo "Deploy echo application"
# From https://raw.githubusercontent.com/cilium/cilium/v1.19/examples/kubernetes/gateway/echo.yaml
kubectl -n "${NAMESPACE}" apply -f - <<EOF
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-1
  name: echo-1
  namespace: dolphin
spec:
  ports:
  - port: 8080
    name: high
    protocol: TCP
    targetPort: 8080
  selector:
    app: echo-1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: echo-1
  name: echo-1
  namespace: dolphin
spec:
  replicas: 1
  selector:
    matchLabels:
      app: echo-1
  template:
    metadata:
      labels:
        app: echo-1
    spec:
      containers:
      - image: gcr.io/kubernetes-e2e-test-images/echoserver:2.2
        name: echo-1
        ports:
        - containerPort: 8080
        env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo-2
  name: echo-2
  namespace: dolphin
spec:
  ports:
  - port: 8090
    name: high
    protocol: TCP
    targetPort: 8080
  selector:
    app: echo-2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: echo-2
  name: echo-2
  namespace: dolphin
spec:
  replicas: 1
  selector:
    matchLabels:
      app: echo-2
  template:
    metadata:
      labels:
        app: echo-2
    spec:
      containers:
      - image: gcr.io/kubernetes-e2e-test-images/echoserver:2.2
        name: echo-2
        ports:
        - containerPort: 8080
        env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
EOF

echo "Verify echo pods are up and running"
NAMESPACE="dolphin"
TIMEOUT=120
INTERVAL=5
wait_for_pods "app=echo-1" "echo-1 pod" || exit 1
wait_for_pods "app=echo-2" "echo-2 pod" || exit 1
echo "✓ Echo pods are running"

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

echo "Deploy gatewayclass"
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: ${GATEWAY_CLASS}
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF

# The GatewayClass must be accepted before listeners and routes can become ready; the helper polls its status with a bounded timeout.
wait_for_gatewayclass_accepted "${GATEWAY_CLASS}" 120 5

#!/usr/bin/env bash
set -euo pipefail

echo "Creating Gateway and HTTPRoute with weights specified"
kubectl -n "${NAMESPACE}" apply -f - <<EOF
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: dolphin-gw
  namespace: dolphin
spec:
  gatewayClassName: dolphin
  listeners:
  - protocol: HTTP
    port: 80
    name: web-gw-echo
    allowedRoutes:
      namespaces:
        from: Same
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: example-route-1
  namespace: dolphin
spec:
  parentRefs:
  - name: dolphin-gw
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /echo
    backendRefs:
    - kind: Service
      name: echo-1
      port: 8080
      weight: 50
    - kind: Service
      name: echo-2
      port: 8090
      weight: 50
EOF

echo "Waiting for Gateway to be programmed and HTTPRoute to be accepted"
GATEWAY_NAME="dolphin-gw"
ROUTE_NAME="example-route-1"
deadline=$((SECONDS + 120))
gateway_ip=""
 
while (( SECONDS < deadline )); do
  gateway_programmed="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' \
      2>/dev/null || true
  )"
 
  gateway_accepted="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.conditions[?(@.type=="Accepted")].status}' \
      2>/dev/null || true
  )"
 
  route_accepted="$(
    kubectl -n "${NAMESPACE}" get httproute "${ROUTE_NAME}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' \
      2>/dev/null || true
  )"
 
  route_refs_resolved="$(
    kubectl -n "${NAMESPACE}" get httproute "${ROUTE_NAME}" \
      -o jsonpath='{.status.parents[0].conditions[?(@.type=="ResolvedRefs")].status}' \
      2>/dev/null || true
  )"
 
  gateway_ip="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.addresses[?(@.type=="IPAddress")].value}' \
      2>/dev/null || true
  )"
 
  if [[ "${gateway_programmed}" == "True" &&
    "${gateway_accepted}" == "True" &&
    "${route_accepted}" == "True" &&
    "${route_refs_resolved}" == "True" &&
    -n "${gateway_ip}" ]]; then
    break
  fi
 
  sleep 5
done
 
# Verify Gateway status
if [[ "${gateway_programmed:-}" != "True" ||
  "${gateway_accepted:-}" != "True" ]]; then
  echo "❌ Gateway did not reach expected status"
  echo "Gateway Programmed: ${gateway_programmed:-False}"
  echo "Gateway Accepted: ${gateway_accepted:-False}"
  kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" -o yaml
  exit 1
fi
 
echo "✓ Gateway status verified:"
echo "  - Accepted: ${gateway_accepted}"
echo "  - Programmed: ${gateway_programmed}"
echo "  - Address: ${gateway_ip}"
 
# Verify HTTPRoute status
if [[ "${route_accepted:-}" != "True" ||
  "${route_refs_resolved:-}" != "True" ]]; then
  echo "❌ HTTPRoute did not reach expected status"
  echo "HTTPRoute Accepted: ${route_accepted:-False}"
  echo "HTTPRoute ResolvedRefs: ${route_refs_resolved:-False}"
  kubectl -n "${NAMESPACE}" get httproute "${ROUTE_NAME}" -o yaml
  exit 1
fi
 
echo "✓ HTTPRoute status verified:"
echo "  - Accepted: ${route_accepted}"
echo "  - ResolvedRefs: ${route_refs_resolved}"
 
# Verify backend refs are properly configured
backend_refs="$(
  kubectl -n "${NAMESPACE}" get httproute "${ROUTE_NAME}" \
    -o jsonpath='{.spec.rules[0].backendRefs}' \
    2>/dev/null || true
)"
 
if [[ -z "${backend_refs}" ]]; then
  echo "❌ HTTPRoute backend references not configured"
  exit 1
fi
 
echo "✓ HTTPRoute backend references configured"
 

echo "Deploy HTTP Route Splitting Cilium envoy config"
kubectl -n dolphin apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/splitting.yaml
sleep 10


echo "Deploying netshoot client"
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
EOF
kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/netshoot --timeout=120s

echo "Gateway address: ${gateway_ip}"
 
echo "Testing traffic split through ${gateway_ip}:80 (from inside netshoot)"
echo "Sending 100 requests and checking 50:50 distribution..."
 
# Execute curl commands from within netshoot and capture results
TOTAL_REQUESTS=100
RESPONSE_FILE="/tmp/curl_responses.txt"
rm -f "${RESPONSE_FILE}"
 
kubectl -n "${NAMESPACE}" exec netshoot -- bash -c "
  for i in {1..${TOTAL_REQUESTS}}; do
    curl -s -k \"http://${gateway_ip}/echo\" >> /tmp/responses.txt
    sleep 0.1
  done
  cat /tmp/responses.txt
" > "${RESPONSE_FILE}"
 
# Count responses from each backend
echo_1_count=$(grep -c "Hostname: echo-1" "${RESPONSE_FILE}" || echo 0)
echo_2_count=$(grep -c "Hostname: echo-2" "${RESPONSE_FILE}" || echo 0)
total_count=$((echo_1_count + echo_2_count))
 
echo ""
echo "Traffic Distribution Results:"
echo "================================"
echo "Total requests: ${total_count}"
echo "Echo-1 responses: ${echo_1_count}"
echo "Echo-2 responses: ${echo_2_count}"
 
if [[ ${total_count} -gt 0 ]]; then
  echo_1_percent=$((echo_1_count * 100 / total_count))
  echo_2_percent=$((echo_2_count * 100 / total_count))
  echo "Echo-1 percentage: ${echo_1_percent}%"
  echo "Echo-2 percentage: ${echo_2_percent}%"
  
  # Check if distribution is approximately 50:50 (allowing 10% deviation)
  if [[ ${echo_1_percent} -ge 40 && ${echo_1_percent} -le 60 && ${echo_2_percent} -ge 40 && ${echo_2_percent} -le 60 ]]; then
    echo "✓ Traffic split is approximately 50:50 ✓"
    exit 0
  else
    echo "❌ Traffic split is NOT 50:50 (expected ~40-60% for each backend)"
    exit 1
  fi
else
  echo "❌ No successful responses received"
  exit 1
fi