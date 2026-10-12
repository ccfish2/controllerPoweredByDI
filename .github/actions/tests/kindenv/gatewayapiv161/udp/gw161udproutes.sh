#!/usr/bin/env bash
source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

echo "Deploying UDP services"
kubectl create ns gateway-conformance-infra --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/udp/deployment.yaml
wait_for_endpoints gateway-conformance-infra udp-backend-host-networking || exit 1

echo "Deploy Gateway, UDP Route"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/udp/gwudp.yaml
# verify gatewayapi through l7 service connection
gatewayip=""
end=$((SECONDS+240))
while true; do
    gatewayip=$(kubectl -n gateway-conformance-infra get gateway host-networking -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

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

#  printf 'hello-from-client\\r\\n' | nc -vuz -w 5 172.18.0.104 53
# Connection to 172.18.0.104 53 port [udp/domain] succeeded!

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

echo "Running UDPTest:"

echo "Executing:"
echo "printf 'hello-from-client\\r\\n' | nc -vuz -w 5 ${gatewayip} 53"

if udp_output="$(
  kubectl -n "${NAMESPACE}" exec "${POD}" -- \
    sh -c 'printf "hello-from-client\r\n" | nc -vuz -w 5 "$1" 53' \
    sh "${gatewayip}" 2>&1
)"; then
    udp_status=0
else
    udp_status=$?
fi

echo "UDPcommand exit status: ${udp_status}"
echo "UDPoutput:"
echo "${udp_output:-<no output>}"

if (( udp_status != 0 )); then
    echo "ERROR: UDPcommand failed (exit status ${udp_status})"
    exit 1
fi

if ! grep -Fq "Gateway API Test UDPServer" <<<"${udp_output}"; then
    echo "ERROR: UDPcommand succeeded, but the expected server response was not found"
    echo "Expected: Gateway API Test UDPServer"
    exit 1
fi

echo "UDPGateway API verification succeeded"
echo "Received expected response: Gateway API Test UDPServer"