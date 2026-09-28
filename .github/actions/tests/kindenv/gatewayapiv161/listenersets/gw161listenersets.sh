
kubectl apply -f /Users/jiminhu/Documents/github.com/controllerPoweredByDI/.github/actions/tests/kindenv/gatewayapiv161/listenersets/listeners.yaml
kubectl create ns listenerset-demo
kubectl -n listenerset-demo apply -f https://raw.githubusercontent.com/cilium/cilium/v1.20/examples/kubernetes/gateway/echo-basic.yaml