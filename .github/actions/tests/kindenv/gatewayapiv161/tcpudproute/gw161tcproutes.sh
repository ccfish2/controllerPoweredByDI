#!/usr/bin/env bash
source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

echo "Deploying TCP and HTTP and services"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tcpudproute/gateway-secrets.yaml
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tcpudproute/deployment.yaml
wait_for_endpoints dolphin tcp-backend || exit 1
wait_for_endpoints dolphin infra-backend-v1 || exit 1

echo "Deploy Gateway, TCP Route and HTTP Route"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tcpudproute/gateway-mixed-http-tcp.yaml
# verify gatewayapi through l7 service connection
gatewayip=""
end=$((SECONDS+120))
while true; do
    gatewayip=$(kubectl -n dolphin get gateway gateway-mixed -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

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

echo "Deploy customized cilium envoy configure"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tcpudproute/tcp-ciliumenvoy.yaml
echo "Wait for envoy configure populate the data"
sleep 60

# printf 'hello-from-client\r\n' | nc -v 10.96.246.79 3000
# Connection to 10.96.246.79 3000 port [tcp/*] succeeded!
# Gateway API Test TCP Server

NAMESPACE="dolphin"
POD="netshoot"

echo "Running TCP Test:"

echo "Executing:"
echo "printf 'hello-from-client\r\n' | nc -v ${gatewayip} 3000"

# Run the TCP test from inside the netshoot pod.
tcp_response="$(
  kubectl -n "${NAMESPACE}" exec "${POD}" -- \
    sh -c "printf 'hello-from-client\r\n' | nc -v ${gatewayip} 3000" \
    2>&1
)"

echo "TCP response:"
echo "${tcp_response}"

# Verify TCP connection succeeded.
if ! echo "${tcp_response}" | grep -q "Connection to ${gatewayip} 3000 port .* succeeded"; then
    echo "ERROR: TCP connection to ${gatewayip}:3000 failed"
    exit 1
fi

# Verify expected Gateway API TCP server response.
if ! echo "${tcp_response}" | grep -q "Gateway API Test TCP Server"; then
    echo "ERROR: TCP server did not return expected response:"
    echo "Expected: Gateway API Test TCP Server"
    echo "Actual:"
    echo "${tcp_response}"
    exit 1
fi

echo "TCP Gateway API verification succeeded"
echo "Received expected response: Gateway API Test TCP Server"

echo
echo "Running HTTP backend verification:"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:80:${gatewayip}" \
  "${URL}"; then
    echo "Backend SVC verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo

else
    echo "ERROR: Backend SVC verification failed"
    exit 1
fi