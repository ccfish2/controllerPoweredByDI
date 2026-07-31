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
echo "deploy gateway http route"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/gatewayhttproute.yaml

# deploy customized envoyconfig yaml
echo "deploy customized envoyconfig yaml"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/envoyconfig.yaml

# verify gateway status and httproute status 
echo "verify httproute status"
wait_for_httproute_ready "dolphin" "https-app-route-1" 120 5 || exit 1

echo "verify gateway status"
verify_gateway_ready "dolphin" "tls-gateway" 120 5 || exit 1
verify_gateway_tls_listener_ready "dolphin" "tls-gateway" "https-1" 120 5 || exit 1

echo "checking gateway svc"
check_service_external_ip "dolphin-ingress-tls-ingress" || exit 1

echo "check dummy endpoints listening on 9999"
verify_gateway_endpoints "dolphin" "dolphin-ingress-tls-ingress" || exit 1

echo "checking dolphin envoy config"
check_dolphin_envoy_config() {
  local namespace="dolphin"
  local resource_name="dolphin-tls-ingress"

  echo "🔍 Checking DolphinEnvoyConfig/$resource_name in namespace $namespace..."

  # Check if the DolphinEnvoyConfig exists
  if ! kubectl -n "$namespace" get CiliumEnvoyConfig "$resource_name" &>/dev/null; then
    echo "❌ Resource DolphinEnvoyConfig/$resource_name does not exist in namespace $namespace."
    return 1
  fi
}
check_dolphin_envoy_config || exit 1

# verify gatewayapi through l7 service connection
end=$((SECONDS+120))
while true; do
    gatewayip=$(kubectl -n dolphin get gateway tls-gateway -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

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

echo "deploy tls gateway and httproute"
DOMAIN="bookinfo.cilium.rocks"
CERT_FILE="${DOMAIN}.pem"
KEY_FILE="${DOMAIN}-key.pem"
 
# --- Generate cert ---
if [[ ! -d mkcert ]]; then
  git clone https://github.com/FiloSottile/mkcert.git
fi
cd mkcert
go build -ldflags "-X main.Version=$(git describe --tags)"
ls -l mkcert
if [[ ! -x ./mkcert ]]; then
  echo "mkcert binary does not exits"
  ls -l
  exit 1
fi

echo "binary mkcert exist"
ls -l mkcert
./mkcert $DOMAIN
kubectl -n dolphin create secret tls tls-secret --cert=bookinfo.cilium.rocks.pem --key=bookinfo.cilium.rocks-key.pem --dry-run=client -o yaml | kubectl apply -f -
cd ..
