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


# dedicated lb mode - each ingress is created with its own loadbalancer service
# deploy multiple ingress and verify each ingress is created with its own loadbalancer service
source "$(dirname "$0")/helper.sh"

# setup end2end with customized agent, envoy, EnvoyConfig
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive # run the agent
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-agent.yaml 
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-envoy.yaml 

# deploy the customzed proxy that points to the ingressIP 
# this should be moved after getting ingress VIP
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-cec.yaml


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
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

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




# Call the function
check_service_external_ip "dolphin-ingress-basic-ingress" || exit 1

kubectl -n dolphin get DolphinEnvoyConfig dolphin-ingress-dolphin-basic-ingress -o yaml

# verify DolphinEnvoyConfig populated as expected
check_ing_dolphin_envoy_config() {
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
  actual_service_name=$(kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" -o jsonpath='{.spec.services[0].name}')
  actual_service_namespace=$(kubectl -n "$namespace" get DolphinEnvoyConfig "$resource_name" -o jsonpath='{.spec.services[0].namespace}')

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
check_ing_dolphin_envoy_config


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

verify_connectivity "$ingressip" || exit 1
echo "clean up dedicated ingress lb mode testing environment"
kubectl -n dolphin delete ingress basic-ingress
kubectl -n dolphin delete CiliumEnvoyConfig cilium-ingress-default-basic-ingress || true
kubectl -n dolphin delete svc dolphin-ingress-basic-ingress || true
time sleep 5

# shared lb mode
#  deploy one LB service dolphin-ingress with external IP into dolphin name space 
#  manually create special Endpoints into dolphin namespace
echo "setup ingress shared lb mode testing environment"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/custom-agent-sharedmode.yaml
kubectl -n kube-system rollout restart ds/cilium
wait_for_pods "k8s-app=cilium" "agent" || exit 1

# switch operator lb-mode to shared mode
kubectl -n dolphin delete sts/operator-dolphin
time sleep 3

helm -n dolphin uninstall dolphin-operator
helm repo add dolphin-operator https://ccfish2.github.io/charts/dolphin-operator/
helm repo update
helm install dolphin-operator dolphin-operator/dolphin-operator --namespace dolphin --create-namespace
helm upgrade --install dolphin-operator dolphin-operator/dolphin-operator -f .github/actions/tests/kindenv/values.yaml --namespace dolphin --create-namespace

kubectl -n dolphin rollout restart sts/operator-dolphin
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-ingress-svc.yaml
# ensure all operator pods are up successfully
time sleep 20

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
#  newly added ingress will be redirect to the dolphin-ingress service, which will be used in CEC route
#  manually create CEC (this CEC will get updated as added ingress) for routing
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-ingress-envoyconfig.yaml

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

verify_connectivity "$ingressip" || exit 1

kubectl apply -f - <<EOF 
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: basic-ingress-shared
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

end=$((SECONDS+120))
while true; do
    ingresssharedlbip=$(kubectl -n dolphin get ingress basic-ingress-shared -o jsonpath="{.status.loadBalancer.ingress[0].ip}")

    if [[ -n "$ingresssharedlbip" ]]; then
        echo "Ingress Service IP acquired: $ingresssharedlbip"
        break
    fi

    echo "Waiting for Ingress IP..."
    sleep 5

    if ((SECONDS > end)); then
        echo "Timeout waiting for Service IP"
        exit 1
    fi
done

if [ "$ingresssharedlbip" != "$ingressip" ]; then
  echo "Error: ingresssharedlbip ($ingresssharedlbip) does not match ingressip ($ingressip)"
  exit 1
else
  echo "✅ IPs match: $ingresssharedlbip"
fi

#!/usr/bin/env bash
echo "clean up shared ingress lb mode testing environment"
kubectl -n dolphin delete ingress basic-ingress-shared || true
kubectl -n dolphin delete ingress basic-ingress || true
kubectl -n dolphin delete svc dolphin-ingress || true
kubectl -n dolphin delete CiliumEnvoyConfig cilium-ingress-default-basic-ingress || true
kubectl -n dolphin delete CiliumEnvoyConfig dolphin-ingress || true
kubectl -n dolphin delete svc dolphin-ingress-basic-ingress || true
helm -n dolphin uninstall dolphin-operator
time sleep 15
echo "end of cleanup"

# below works as expected using local Kind cluster 
echo "helmp upgrade dolphin-operator"
helm repo update
helm install dolphin-operator dolphin-operator/dolphin-operator -n dolphin
sleep 5
echo "Deploying a TLS-enabled ingress and validating HTTPS reconciliation"
 
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
 
# --- Push cert material into cilium-secrets so Cilium's SDS watcher (envoy-secrets-namespace) picks it up ---
kubectl create namespace cilium-secrets --dry-run=client -o yaml | kubectl apply -f -
 
kubectl -n cilium-secrets delete secret tls-ingress-secret --ignore-not-found
kubectl -n cilium-secrets create secret tls tls-ingress-secret \
  --cert=$CERT_FILE \
  --key=$KEY_FILE
 
# --- CA configmap for the in-cluster verification pod ---
echo "create configmap that persist the cacert for accessing service"
kubectl -n dolphin create configmap bookinfo-ca --from-file=bookinfo.cilium.rocks.pem=bookinfo.cilium.rocks.pem

cd ..

# --- Apply ingress config ---
echo "deploy tls-ingress and the 9999 cilium port "
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress.yaml
 
#--- Wait for TLS ingress LoadBalancer IP VIP  ---
end=$((SECONDS + 120))
tlsingressip=""
while true; do
    tlsingressip=$(kubectl -n dolphin get ingress tls-ingress \
      -o jsonpath="{.status.loadBalancer.ingress[0].ip}" 2>/dev/null || true)
 
    if [[ -n "$tlsingressip" ]]; then
        echo "TLS Ingress Service IP acquired: $tlsingressip"
        break
    fi
 
    echo "Waiting for TLS Ingress IP..."
    sleep 5
 
    if ((SECONDS > end)); then
        echo "Timeout waiting for TLS Ingress LB IP"
        exit 1
    fi
done
 
# External connectivity check — confirm this helper does SNI/Host to $DOMAIN, not a hardcoded name
#verify_https_connectivity "$ingressip" "$DOMAIN" || exit 1
echo "apply cilium envoy configure  for tls-ingress routing and TLS termination"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress-envoyconfig.yaml

# --- In-cluster cert-validated verification via busybox pod ---
# curl -k https://bookinfo.cilium.rocks/details/1 --resolve bookinfo.cilium.rocks:443:{TLS INGRESS VIP} -v 
# {"id":1,"author":"William Shakespeare","year":1595,"type":"paperback","pages":200,"publisher":"PublisherA","language":"English","ISBN-10":"1234567890","ISBN-13":"123-1234567890"}
kubectl -n dolphin delete pod busybox --ignore-not-found --wait=true
 
set -uo pipefail

NAMESPACE="dolphin"
POD="netshoot"
HOST="bookinfo.cilium.rocks"
CACERT="/certs/${HOST}.pem"
URL="https://${HOST}/details/1"

echo "NAMESPACE=${NAMESPACE}"
echo "POD=${POD}"
echo "HOST=${HOST}"
echo "CACERT=${CACERT}"
echo "URL=${URL}"

kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  labels:
    run: netshoot
  name: netshoot
  namespace: dolphin
spec:
  containers:
  - command:
    - sleep
    - infinity
    image: nicolaka/netshoot
    name: netshoot
    volumeMounts:
    - name: ca-cert
      mountPath: /certs
  volumes:
  - name: ca-cert
    configMap:
      name: bookinfo-ca
EOF

kubectl -n "${NAMESPACE}" wait \
  --for=condition=Ready \
  pod/"${POD}" \
  --timeout=60s

WAIT_STATUS=$?

if [[ $WAIT_STATUS -ne 0 ]]; then
  echo "ERROR: netshoot verification pod did not reach Succeeded phase"
  kubectl -n "${NAMESPACE}" describe pod "${POD}" || true
  kubectl -n "${NAMESPACE}" logs "${POD}" || true
  exit 1
fi

echo "Resolving ${HOST} -> ${tlsingressip}"
if [[ -z "${tlsingressip:-}" ]]; then
    echo "ERROR: tlsingressip is not set"
    exit 1
fi

RESPONSE=$(kubectl -n "${NAMESPACE}" exec "${POD}" -- \
    curl -sS -o /tmp/response.json -w "%{http_code}" \
    --resolve "${HOST}:443:${tlsingressip}" \
    --cacert "${CACERT}" \
    "${URL}")
CURL_EXIT=$?

if [[ $CURL_EXIT -ne 0 ]]; then
    echo "ERROR: curl failed with exit code ${CURL_EXIT} (TLS handshake / connection issue)"
    exit 1
fi

if [[ "${RESPONSE}" != "200" ]]; then
    echo "ERROR: unexpected HTTP status ${RESPONSE}"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json || true
    exit 1
fi

echo "TLS ingress verification succeeded (HTTP ${RESPONSE})"
kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
echo
###
# * Added bookinfo.cilium.rocks:443:172.19.0.101 to DNS cache
# * Hostname bookinfo.cilium.rocks was found in DNS cache
# * Host bookinfo.cilium.rocks:443 was resolved.
# * IPv6: (none)
# * IPv4: 172.19.0.101
# *   Trying 172.19.0.101:443...
# * ALPN: curl offers h2,http/1.1
# * TLSv1.3 (OUT), TLS handshake, Client hello (1):
# * SSL Trust Anchors:
# *   CAfile: /certs/bookinfo.cilium.rocks.pem
# *   CApath: /etc/ssl/certs
# * TLSv1.3 (IN), TLS handshake, Server hello (2):
# * TLSv1.3 (IN), TLS change cipher, Change cipher spec (1):
# * TLSv1.3 (IN), TLS handshake, Encrypted Extensions (8):
# * TLSv1.3 (IN), TLS handshake, Certificate (11):
# * TLSv1.3 (IN), TLS handshake, CERT verify (15):
# * TLSv1.3 (IN), TLS handshake, Finished (20):
# * TLSv1.3 (OUT), TLS change cipher, Change cipher spec (1):
# * TLSv1.3 (OUT), TLS handshake, Finished (20):
# * SSL connection using TLSv1.3 / TLS_AES_256_GCM_SHA384 / x25519 / RSASSA-PSS
# * ALPN: server did not agree on a protocol. Uses default.
# * Server certificate:
# *   subject: O=mkcert development certificate; OU=jiminhu@Jimins-MacBook-Air.local (Jimin Hu)
# *   start date: Aug  4 18:52:40 2026 GMT
# *   expire date: Nov  4 18:52:40 2028 GMT
# *   issuer: O=mkcert development CA; OU=jiminhu@Jimins-MacBook-Air.local (Jimin Hu); CN=mkcert jiminhu@Jimins-MacBook-Air.local (Jimin Hu)
# *   Certificate level 0: Public key type RSA (2048/112 Bits/secBits), signed using sha256WithRSAEncryption
# *   subjectAltName: "bookinfo.cilium.rocks" matches cert's "bookinfo.cilium.rocks"
# * OpenSSL verify result: 0
# * SSL certificate verified via OpenSSL.
# * Established connection to bookinfo.cilium.rocks (172.19.0.101 port 443) from 10.244.1.189 port 39836 
# * using HTTP/1.x
# > GET /details/1 HTTP/1.1
# > Host: bookinfo.cilium.rocks
# > User-Agent: curl/8.21.0
# > Accept: */*
# > 
# * Request completely sent off
# * TLSv1.3 (IN), TLS handshake, Newsession Ticket (4):
# * TLSv1.3 (IN), TLS handshake, Newsession Ticket (4):
# < HTTP/1.1 200 OK
# < content-type: application/json
# < server: envoy
# < date: Tue, 04 Aug 2026 23:07:14 GMT
# < content-length: 178
# < x-envoy-upstream-service-time: 4
# < 
# * Connection #0 to host bookinfo.cilium.rocks:443 left intact
# {"id":1,"author":"William Shakespeare","year":1595,"type":"paperback","pages":200,"publisher":"PublisherA","language":"English","ISBN-10":"1234567890","ISBN-13":"123-1234567890"}
echo "TLS ingress verification succeeded"

# we could verify migrated tls-ingress to tls-gateway and gateway access works as expected
# .github/actions/tests/kindenv/ingressintegrationtests_setup/tlsingress-migratetogatewayapi
# apply gateway and httproute, apply ciliumenvoyconfig for tls-gateway
# verfiication similar as above TLS-ingress, only need tls-gateway-externalIP
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/details/1
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/productpage

# --- Cleanup ---
kubectl -n dolphin delete pod netshoot --ignore-not-found
kubectl -n dolphin delete configmap bookinfo-ca --ignore-not-found
kubectl -n cilium-secrets delete secret tls-ingress-secret --ignore-not-found
kubectl delete -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress-envoyconfig.yaml --ignore-not-found
cd ..
rm -rf mkcert