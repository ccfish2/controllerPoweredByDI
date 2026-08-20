#!/usr/bin/env bash
# filepath: controllerPoweredByDI/.github/actions/tests/kindenv/lib/metallb.sh

get_node10ips() {
  # Extract the IPv4 subnet using jq
  subnet=$(docker network inspect kind | jq -r '.[0].IPAM.Config[] | select(.Subnet | test("^\\d+\\.\\d+\\.\\d+\\.\\d+/\\d+$")) | .Subnet')

  # Get the IP prefix (first three octets)
  ip_prefix=$(echo "$subnet" | cut -d. -f1-3)

  # Construct and echo the IP range
  echo "${ip_prefix}.100 - ${ip_prefix}.110"
}
ips=$(get_node10ips)

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/helper.sh"

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
kubectl create deployment nginx --image=nginx || true
kubectl expose deployment nginx --port=80 --type=LoadBalancer || true

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


# Validate internal nginx connectivity
end=$((SECONDS + 120))

kubectl wait \
  --for=condition=available \
  deployment/nginx \
  --timeout=120s

wait_for_endpoints default nginx 120

while (( SECONDS <= end )); do
  echo "Checking internal connectivity to nginx service..."

  if output=$(kubectl run "curl-nginx-$RANDOM" \
      --rm \
      -i \
      --restart=Never \
      --image=curlimages/curl \
      --quiet \
      -- \
      curl -sS --fail --connect-timeout 5 --max-time 10 \
      http://nginx.default.svc.cluster.local 2>/dev/null); then

    if grep -q "Welcome to nginx" <<<"$output"; then
      echo "✅ Service is reachable."
      break
    fi
  else
    echo "⚠️ nginx connectivity probe failed; retrying..."
  fi

  echo "Service not reachable yet. Waiting 5 seconds..."
  sleep 5
done

if (( SECONDS > end )); then
  echo "❌ Timeout waiting for nginx service to be reachable."
  kubectl get svc nginx
  kubectl get endpoints nginx
  exit 1
fi