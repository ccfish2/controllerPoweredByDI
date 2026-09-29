#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"
echo "Deploying shared gateway into dolphin namespace, http routes and ListenerSet into listenerset-demo namespace"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/listenersets/listeners.yaml
echo "Waiting for Gateway to be programmed and HTTPRoute to be accepted"
GATEWAY_NAME="shared-gateway"
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
 
  gateway_ip="$(
    kubectl -n "${NAMESPACE}" get gateway "${GATEWAY_NAME}" \
      -o jsonpath='{.status.addresses[?(@.type=="IPAddress")].value}' \
      2>/dev/null || true
  )"
 
  if [[ "${gateway_programmed}" == "True" &&
    "${gateway_accepted}" == "True" &&
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

echo "Deploy cilium envoy configuration"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/listenersets/listenersets-cec.yaml

echo "Deploy echo-1 and echo-2 services"
kubectl -n listenerset-demo apply -f https://raw.githubusercontent.com/cilium/cilium/v1.20/examples/kubernetes/gateway/echo-basic.yaml
echo "Verify echo pods are up and running"
NAMESPACE="listenerset-demo"
TIMEOUT=120
INTERVAL=5
wait_for_pods "app=echo-1" "echo-1 pod" || exit 1
wait_for_pods "app=echo-2" "echo-2 pod" || exit 1
echo "✓ Echo pods are running"

NAMESPACE="dolphin"
POD="netshoot"
if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
    curl -sSL \
    -o /tmp/response.json \
    -w "%{http_code}" \
    --fail \
    --header 'Host: echo.example.com' \
    "http://${gateway_ip}:8081"
then
    echo "ListenerSets verification succeeded (HTTP 200)"

    kubectl -n "${NAMESPACE}" exec "${POD}" -- \
        cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi