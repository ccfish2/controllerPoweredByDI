#!/usr/bin/env bash

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: "dolphin"
spec:
  controllerName: io.dolphin/gateway-controller
  description: The default Dolphin GatewayClass
EOF

echo "verify gateway class is accepted"
wait_for_gatewayclass_accepted "dolphin" 120 5 || exit 1

kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/http/gwhttp.yaml