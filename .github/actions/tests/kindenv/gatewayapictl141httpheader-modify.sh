#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

echo "Deploy echo application"
# From https://raw.githubusercontent.com/cilium/cilium/1.20.1/examples/kubernetes/gateway/echo-basic.yaml
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
    targetPort: 3000
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
      - image: registry.k8s.io/gateway-api/echo-basic:v1.5.1
        name: echo-1
        ports:
        - containerPort: 3000
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
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
    targetPort: 3000
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
      - image: registry.k8s.io/gateway-api/echo-basic:v1.5.1
        name: echo-2
        ports:
        - containerPort: 3000
        env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
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

echo "Creating Gateway and HTTPRoute with header modifier request"
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
  name: header-http-echo
  namespace: dolphin
spec:
  parentRefs:
    - name: dolphin-gw
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /add-a-request-header
      filters:
        - type: RequestHeaderModifier
          requestHeaderModifier:
            add:
              - name: my-header-name
                value: my-header-value
      backendRefs:
        - name: echo-1
          port: 8080
EOF

echo "Waiting for Gateway to be programmed and HTTPRoute to be accepted"
GATEWAY_NAME="dolphin-gw"
ROUTE_NAME="header-http-echo"
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
kubectl -n dolphin apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/httpmodify-header-cec.yaml
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

echo "Testing http request modified (from inside netshoot)"
response="$(kubectl -n "${NAMESPACE}" exec netshoot -- curl -s -k "http://${gateway_ip}/add-a-request-header")"
echo "${response}"

# Verify the custom header is present in the response
if echo "${response}" | grep -q "My-Header-Name"; then
  echo "✓ Custom header 'My-Header-Name' found in response"
else
  echo "✗ Custom header 'My-Header-Name' NOT found in response"
  exit 1
fi