#!/usr/bin/env bash

# deploy metalb
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml

# apply metallb-config.yaml with address pool
kubectl apply -f - <<EOF
apiVersion: v1
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - 192.168.56.240-192.168.56.250
kind: ConfigMap
metadata:
    name: config
    namespace: metallb-system
EOF

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

# monitor controller status
timeout 120 bash -c 'until kubectl get pod -n metallb-system $(kubectl get pods -n metallb-system -o name | grep controller | cut -d/ -f2) -o jsonpath="{.status.containerStatuses[0].ready}" | grep true; do echo "Waiting for controller pod..."; sleep 5; done'
echo "metalb controller up and running"

# monitor speaker status
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

# validate metalb works for nginx test case
kubectl deploy nginx --image=nginx 
kubectl expose deployment nginx --port=80 --type=LoadBalancer
timeout 120 bash -c 'until kubectl get svc nginx -o jsonpath="{.status.loadBalancer.ingress[0].ip}" | grep -E "192\.168\.56\.[2-5][0-9]"; do echo "Waiting for LoadBalancer IP..."; sleep 5; done'
nginx_ip=$(kubectl get svc nginx -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
output=$(nc -zv $nginx_ip 80 2>&1)
if echo "$output" | grep -q "succeeded"; then
    echo "MetalLB is working correctly. LoadBalancer IP: $nginx_ip"
else
    echo "MetalLB is not working correctly."
    exit 1
fi

# apply basic-ingress
# ensure ingress-controller reconcile
# loadbalancer service is up, external ip is assigned to the ingress, DolphinEnvoyConfig is created and with correct status
