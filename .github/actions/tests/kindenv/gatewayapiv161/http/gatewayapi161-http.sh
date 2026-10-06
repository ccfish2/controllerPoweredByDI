#!/usr/bin/env bash

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/http/gwhttp.yaml