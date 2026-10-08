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
end=$((SECONDS+240))
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

if ! kubectl -n "${NAMESPACE}" get pod "${POD}" >/dev/null 2>&1; then
    echo "Pod ${NAMESPACE}/${POD} is missing; creating it..."
    kubectl -n "${NAMESPACE}" run "${POD}" \
        --image=nicolaka/netshoot \
        --restart=Never \
        --command -- sleep infinity || {
            echo "ERROR: Failed to create ${NAMESPACE}/${POD}"
            exit 1
        }
fi

if ! kubectl -n "${NAMESPACE}" wait \
    --for=condition=Ready "pod/${POD}" --timeout=120s; then
    echo "ERROR: ${NAMESPACE}/${POD} did not become ready"
    kubectl -n "${NAMESPACE}" describe pod "${POD}" || true
    kubectl -n "${NAMESPACE}" logs "${POD}" --all-containers=true || true
    exit 1
fi
echo "Pod ${NAMESPACE}/${POD} is ready"

echo "Running TCP Test:"

echo "Executing:"
echo "printf 'hello-from-client\\r\\n' | nc -v -w 5 ${gatewayip} 3000"

if tcp_output="$(
  kubectl -n "${NAMESPACE}" exec "${POD}" -- \
    sh -c 'printf "hello-from-client\r\n" | nc -v -w 5 "$1" 3000' \
    sh "${gatewayip}" 2>&1
)"; then
    tcp_status=0
else
    tcp_status=$?
fi

echo "TCP command exit status: ${tcp_status}"
echo "TCP output:"
echo "${tcp_output:-<no output>}"

if (( tcp_status != 0 )); then
    echo "ERROR: TCP command failed (exit status ${tcp_status})"
    exit 1
fi

if ! grep -Fq "Gateway API Test TCP Server" <<<"${tcp_output}"; then
    echo "ERROR: TCP command succeeded, but the expected server response was not found"
    echo "Expected: Gateway API Test TCP Server"
    exit 1
fi

echo "TCP Gateway API verification succeeded"
echo "Received expected response: Gateway API Test TCP Server"