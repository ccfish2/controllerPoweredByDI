#!/usr/bin/env bash

get_node10ips() {
  # Extract the IPv4 subnet using jq
  subnet=$(docker network inspect kind | jq -r '.[0].IPAM.Config[] | select(.Subnet | test("^\\d+\\.\\d+\\.\\d+\\.\\d+/\\d+$")) | .Subnet')

  # Get the IP prefix (first three octets)
  ip_prefix=$(echo "$subnet" | cut -d. -f1-3)

  # Construct and echo the IP range
  echo "${ip_prefix}.100 - ${ip_prefix}.110"
}
ips=$(get_node10ips)

# deploy metalb
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml

# apply metallb configmap
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - "$ips"
EOF

# monitor metallb controller status
end=$((SECONDS+120))

# monitor controller status
until kubectl get pod -n metallb-system \
    $(kubectl get pods -n metallb-system -o name | grep controller | cut -d/ -f2) \
    -o jsonpath="{.status.containerStatuses[0].ready}" | grep true
do 
    echo "Waiting for controller pod..."
    sleep 5
    if ((SECONDS > end)); then
        echo "Timeout waiting for controller pod"
        exit 1
    fi
done

# monitor metallb speaker status
NAMESPACE="metallb-system"
TIMEOUT=120
INTERVAL=5
ELAPSED=0

echo "waiting for all 'speaker' pods to be ready... (timeout: $TIMEOUT seconds)"

while [ $ELAPSED -lt $TIMEOUT ]; do
    NOT_READY=$(kubectl get pods -n $NAMESPACE -l app=metallb,component=speaker -o jsonpath='{.items[?(@.status.containerStatuses[0].ready==false)].metadata.name}')
    if [ -z "$NOT_READY" ]; then
        echo "All 'speaker' pods are ready"
        break
    else
        echo "Waiting for 'speaker' pods to be ready..."
        sleep $INTERVAL
        ELAPSED=$((ELAPSED + INTERVAL))
    fi
done

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "Timeout waiting for 'speaker' pods to be ready"
    kubectl get pods -n $NAMESPACE -l app=metallb,component=speaker
    exit 1
fi

# apply metalb ipaddresspool
timeout=120  # seconds
interval=5   # seconds between attempts
start_time=$(date +%s)

# Apply the resource
while true; do
    kubectl apply -f - <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default-address-pool
  namespace: metallb-system
spec:
  addresses:
  - "$ips"
EOF

    # Check if resource exists
    if kubectl get ipaddresspool -n metallb-system default-address-pool &> /dev/null; then
        echo "Resource successfully deployed."
        break
    fi

    # Check for timeout
    current_time=$(date +%s)
    elapsed=$(( current_time - start_time ))
    if [ "$elapsed" -ge "$timeout" ]; then
        echo "Timed out after $timeout seconds. Resource not confirmed as deployed."
        exit 1
    fi

    echo "Retrying in $interval seconds..."
    sleep "$interval"
done

# apply excludel2.yaml to exclude interfaces
kubectl apply -f - <<EOF
apiVersion: v1
data:
  excludel2.yaml: |
    announcedInterfacesToExclude: ["^docker.*", "^cbr.*", "^dummy.*", "^virbr.*", "^lxcbr.*", "^veth.*", "^lo$", "^cali.*", "^tunl.*", "^flannel.*", "^kube-ipvs.*", "^cni.*", "^nodelocaldns.*"]
kind: ConfigMap
metadata:
  name: metallb-excludel2
  namespace: metallb-system
EOF


# validate metalb works for nginx test case
kubectl create deployment nginx --image=nginx 
kubectl expose deployment nginx --port=80 --type=LoadBalancer

end=$((SECONDS+120))
while true; do
    ip=$(kubectl get svc nginx -o jsonpath="{.status.loadBalancer.ingress[0].ip}")

    if [[ -n "$ip" ]]; then
        echo "LoadBalancer IP acquired: $ip"
        break
    fi

    echo "Waiting for LoadBalancer IP..."
    sleep 5

    if ((SECONDS > end)); then
        echo "Timeout waiting for LoadBalancer IP"
        exit 1
    fi
done


end=$((SECONDS+120))
while true; do
  echo "Checking internal connectivity to nginx service..."
  output=$(kubectl run busybox --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s http://nginx.default.svc.cluster.local)

  if echo "$output" | grep -q "Welcome to nginx"; then
    echo "Service is reachable. Skipping LoadBalancer IP check in CI."
    break
  fi 

  echo "Service not reachable yet. wait for 5 seconds..."
  sleep 5
  
  if ((SECONDS > end)); then
    echo "Timeout waiting for service to be reachable"
    exit 1
  fi
done


# setup end2end with customized agent, envoy, EnvoyConfig
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive

kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-agent.yaml 
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-envoy.yaml 

echo "Checking kube-system cilium agent and envoy pods are ready"
NAMESPACE="kube-system"
TIMEOUT=120
INTERVAL=5

wait_for_pods() {
    local LABEL=$1
    local DESCRIPTION=$2
    local elapsed=0

    echo "Waiting for all '${DESCRIPTION}' pods to be ready... (timeout: ${TIMEOUT} seconds)"
    while [ $elapsed -lt $TIMEOUT ]; do
        NOT_READY=$(kubectl get pods -n "$NAMESPACE" -l "$LABEL" -o jsonpath='{.items[?(@.status.containerStatuses[0].ready==false)].metadata.name}')
        if [ -z "$NOT_READY" ]; then
            echo "All '${DESCRIPTION}' pods are ready"
            return 0
        else
            echo "Waiting for '${DESCRIPTION}' pods to be ready..."
            sleep $INTERVAL
            elapsed=$((elapsed + INTERVAL))
        fi
    done

    echo "Timeout waiting for '${DESCRIPTION}' pods to be ready"
    kubectl get pods -n "$NAMESPACE" -l "$LABEL"
    return 1
}

# Check both agent and envoy pods
wait_for_pods "k8s-app=cilium" "agent" || exit 1
wait_for_pods "k8s-app=cilium-envoy" "envoy" || exit 1

echo "Verify Gateway API end2end"
source "$(dirname "$0")/gatewayapi_setup.sh"
source "$(dirname "$0")/gatewayapi_setup.sh"

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
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/gatewayhttproute.yaml

# deploy customized envoyconfig yaml
echo "deploy customized envoyconfig yaml"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/envoyconfig.yaml

# verify gateway status and httproute status 
echo "verify httproute status"
wait_for_httproute_ready "dolphin" "http-app-1" 120 5 || exit 1

echo "verify gateway status"
verify_gateway_ready "dolphin" "my-gateway" 120 5 || exit 1

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

    if [[ -n "$ingressip" ]]; then
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