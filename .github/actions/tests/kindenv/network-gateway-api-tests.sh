#!/usr/bin/env bash


echo "Verify Gateway API end2end"
source "$(dirname "$0")/gatewayapi_setup.sh"
source "$(dirname "$0")/helper.sh"

# This script sets up a Gateway API environment in a Kind cluster.
echo "deply services in the same namespace"
kubectl -n dolphin apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

echo "deploy gateway class"
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: dolphin
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF
echo "verify gateway class is accepted"
wait_for_gatewayclass_accepted "dolphin" 120 5 || exit 1

# deploy gateway and httproute
echo "deploy gateway and httproute"
openssl req -x509 -nodes -newkey rsa:2048 -days 1 -keyout /tmp/gateway-tls.key -out /tmp/gateway-tls.crt -subj "/CN=example.com" >/dev/null 2>&1
kubectl -n dolphin create secret tls test-tls-secret --cert=/tmp/gateway-tls.crt --key=/tmp/gateway-tls.key --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/gatewayhttproute.yaml

rm -f /tmp/gateway-tls.crt /tmp/gateway-tls.key

# deploy customized envoyconfig yaml
echo "deploy customized envoyconfig yaml"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/envoyconfig.yaml

# verify gateway status and httproute status 
echo "verify httproute status"
wait_for_httproute_ready "dolphin" "http-app-1" 120 5 || exit 1

echo "verify gateway status"
verify_gateway_ready "dolphin" "my-gateway" 120 5 || exit 1
verify_gateway_tls_listener_ready "dolphin" "my-gateway" "https" 120 5 || exit 1

echo "checking gateway svc"
check_service_external_ip "dolphin-gateway-my-gateway" || exit 1

echo "check dummy endpoints listening on 9999"
verify_gateway_endpoints "dolphin" "dolphin-gateway-my-gateway" || exit 1

echo "checking dolphin envoy config"
check_dolphin_envoy_config() {
  local namespace="dolphin"
  local resource_name="dolphin-gateway-my-gateway"

  echo "🔍 Checking DolphinEnvoyConfig/$resource_name in namespace $namespace..."

  # Check if the DolphinEnvoyConfig exists
  if ! kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" &>/dev/null; then
    echo "❌ Resource DolphinEnvoyConfig/$resource_name does not exist in namespace $namespace."
    return 1
  fi
}
check_dolphin_envoy_config || exit 1

# verify gatewayapi through l7 service connection
end=$((SECONDS+120))
while true; do
    gatewayip=$(kubectl -n dolphin get gateway my-gateway -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

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

verify_connectivity "$gatewayip" || exit 1