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

# echo "NGINX got LoadBalancer IP"
# nginx_ip=$(kubectl get svc nginx -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
# echo "Checking connectivity to $nginx_ip on port 80..."

# # Run the nc command from a busybox pod
# output=$(kubectl run tmp-busybox --rm -i --restart=Never --image=busybox:1.28 -- nc -zv "$nginx_ip" 80 2>&1)

# echo "$output"

# if echo "$output" | grep -q "open"; then
#     echo "MetalLB is working correctly. LoadBalancer IP: $nginx_ip"
# else
#     echo "MetalLB is not working correctly."
#     exit 1
# fi

# apply ingressClass
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: dolphin
spec:
  controller: dolphin.io/ingress-controller
EOF

# apply book info service 
#kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

# apply basic-ingress
kubectl apply -f - <<EOF 
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: basic-ingress
  namespace: dolphin
spec:
  ingressClassName: dolphin
  rules:
  - http:
      paths:
      - backend:
          service:
            name: details
            port:
              number: 9080
        path: /details
        pathType: Prefix
      - backend:
          service:
            name: productpage
            port:
              number: 9080
        path: /
        pathType: Prefix
EOF

# ensure ingress-controller reconcile
# loadbalancer service is up, external ip is assigned to the ingress, DolphinEnvoyConfig is created and with correct status
end=$((SECONDS+120))
while true; do
    ingressip=$(kubectl -n dolphin get ingress basic-ingress -o jsonpath="{.status.loadBalancer.ingress[0].ip}")

    if [[ -n "$ingressip" ]]; then
        echo "Ingress Service IP acquired: $ingressip"
        break
    fi

    echo "Waiting for Ingress IP..."
    sleep 5

    if ((SECONDS > end)); then
        echo "Timeout waiting for Service IP"
        exit 1
    fi
done

# verify DolphinEnvoyConfig populated as expected
check_dolphin_envoy_config() {
  local namespace="dolphin"
  local resource_name="dolphin-ingress-dolphin-basic-ingress"
  local expected_service_name="dolphin-ingress-basic-ingress"
  local expected_service_namespace="dolphin"

  echo "🔍 Checking DolphinEnvoyConfig/$resource_name in namespace $namespace..."

  # Check if the DolphinEnvoyConfig exists
  if ! kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" &>/dev/null; then
    echo "❌ Resource DolphinEnvoyConfig/$resource_name does not exist in namespace $namespace."
    return 1
  fi

  local actual_service_name actual_service_namespace
  actual_service_name=$(kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" -o jsonpath='{.services[0].name}')
  actual_service_namespace=$(kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" -o jsonpath='{.services[0].namespace}')

  if [[ "$actual_service_name" == "$expected_service_name" && \
        "$actual_service_namespace" == "$expected_service_namespace" ]]; then
    echo "✅ Resource and expected services entry found."
    return 0
  else
    echo "❌ Resource exists but expected services entry is missing or incorrect."
    echo "    Expected: name=$expected_service_name, namespace=$expected_service_namespace"
    echo "    Actual:   name=$actual_service_name, namespace=$actual_service_namespace"
    return 2
  fi
}
check_dolphin_envoy_config


# setup end2end with customized agent, envoy, EnvoyConfig
kubectl apply -f ./ingressintegrationtests_setup/crds/ --recursive

kubectl apply -f ./ingressintegrationtests_setup/custom-agent.yaml 
# ensure aget is running and ready
kubectl apply -f ./ingressintegrationtests_setup/custom-envoy.yaml 
# ensure envoy is running and ready

# deploy the customzed envoy that points to the ingressIP 
kubectl apply -f ./ingressintegrationtests_setup/custom-cec.yaml


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

# verify through l7 service connection
end=$((SECONDS+120))
while true; do
  echo "Checking internal connectivity to basic-ingress service..."
  output=$(kubectl run busybox --rm -i --restart=Never --image=curlimages/curl -- \
        curl -s --fail -v http://$ingressip/details/1)
  
  if echo "$output" | grep -q "William Shakespeare"; then
    echo "Ingress LoadBalancer Service is reachable."
    break
  fi 

  echo "Ingress LoadBalancer Service not reachable yet. wait for 5 seconds..."
  sleep 5
  
  if ((SECONDS > end)); then
    echo "Timeout waiting for Ingress LoadBalancer Service to be reachable"
    exit 1
  fi
done
