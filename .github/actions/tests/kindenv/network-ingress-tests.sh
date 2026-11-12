#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/ingress_check.sh"

# test case one: dedicated lb ingress
# apply book info service into dolphin namespace
kubectl -n dolphin apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

# backends must be running before the route can work
kubectl -n dolphin wait --for=condition=Available \
  deployment/details-v1 deployment/productpage-v1 --timeout=300s

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
---
addressType: IPv4
apiVersion: discovery.k8s.io/v1
endpoints:
- addresses:
  - 192.192.192.192
  conditions:
    ready: true
kind: EndpointSlice
metadata:
  name: dolphin-ingress-basic-ingress
  namespace: dolphin
ports:
- name: ""
  port: 9999
  protocol: TCP
EOF

# ensure ingress-controller reconcile and the cilium data plane is programmed:
#   - CiliumEnvoyConfig is created by the operator and is consistent (routes -> clusters, no proxy_protocol)
#   - backends have ready endpoints
#   - loadbalancer service is up, external ip is assigned to the ingress
#   - cilium-agent redirects the ingress IP to envoy on every node
#   - the route answers correctly several times in a row
# The address is printed on stdout, the progress goes to stderr.
IC_NAMESPACE=dolphin
IC_INGRESS=basic-ingress
IC_BACKEND_SERVICES="details productpage"
IC_URL_PATH=/details/1
IC_EXPECT="William Shakespeare"

ingressip=$(ic_check_ingress_ready) || {
  echo "ERROR: ingress ${IC_NAMESPACE}/${IC_INGRESS} is not ready in the data plane" >&2
  exit 1
}
echo "Ingress is ready at: ${ingressip}"

# echo "clean up dedicated ingress lb mode testing environment"
kubectl -n dolphin delete ingress basic-ingress --wait=true
kubectl -n dolphin delete dolphinenvoyconfigs.dolphin.io dolphin-ingress --ignore-not-found=true --wait=true
kubectl -n dolphin delete svc dolphin-ingress-basic-ingress --ignore-not-found=true --wait=true
# time sleep 5

# test case 2: shared lb mode
#  deploy one LB service dolphin-ingress with external IP into dolphin name space 
#  manually create special Endpoints into dolphin namespace
echo "setup ingress shared lb mode testing environment"
cilium config set ingress-default-lb-mode shared 
kubectl -n kube-system rollout restart ds/cilium 
kubectl -n kube-system rollout restart ds/cilium-envoy 
wait_for_pods "k8s-app=cilium" "agent" || exit 1

# switch operator and dataplane lb-mode to shared mode
echo "switch dataplane supporting shared lb mode"
helm repo update
helm upgrade dolphin-operator dolphin-operator/dolphin-operator --namespace dolphin \
          --reuse-values \
          --set ingressController.loadbalancerMode=shared
kubectl -n dolphin rollout restart sts/operator-dolphin

echo "deploying services"
kubectl apply -f - <<EOF 
apiVersion: v1
kind: Service
metadata:
  name: dolphin-ingress
  namespace: dolphin
spec:
  externalTrafficPolicy: Cluster
  internalTrafficPolicy: Cluster
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  ports:
  - name: http
    nodePort: 31605
    port: 80
    protocol: TCP
    targetPort: 80
  - name: https
    nodePort: 30965
    port: 443
    protocol: TCP
    targetPort: 443
  sessionAffinity: None
  type: LoadBalancer
--- 
apiVersion: v1
kind: Endpoints
metadata:
  name: dolphin-ingress
  namespace: dolphin
subsets:
- addresses:
  - ip: 192.192.192.192
  ports:
  - port: 9999
    protocol: TCP
EOF
#kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-ingress-svc.yaml
time sleep 20

ingressip=""
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

echo "Deploy ingress for shared lb mode"
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

ingresssharedlbip=""
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
#echo "clean up shared ingress lb mode testing environment"
kubectl -n dolphin delete ingress basic-ingress-shared --ignore-not-found=true --wait=true
kubectl -n dolphin delete ingress basic-ingress --ignore-not-found=true --wait=true
kubectl -n dolphin delete svc dolphin-ingress --ignore-not-found=true --wait=true
kubectl -n dolphin delete svc dolphin-ingress-basic-ingress --ignore-not-found=true --wait=true
#uninstall_helm_release dolphin-operator dolphin
set -euo pipefail
NS="dolphin"
RELEASE="dolphin-operator"
CHART="dolphin-operator/dolphin-operator"
TIMEOUT="5m"
# Set to "true" only if you want CRDs (and all their custom resources) deleted
DELETE_CRDS="false"
# Pattern to identify the operator's CRDs (adjust to match your chart)
CRD_PATTERN="dolphin"

log() { echo "[$(date +%H:%M:%S)] $*"; }

# ---------- 1. Uninstall ----------
if helm -n "$NS" status "$RELEASE" >/dev/null 2>&1; then
  log "Uninstalling $RELEASE..."
  helm -n "$NS" uninstall "$RELEASE" --wait --timeout "$TIMEOUT"
else
  log "Release $RELEASE not found in $NS, skipping uninstall."
fi

# ---------- 2. Verify release is gone ----------
if helm -n "$NS" list -q | grep -qx "$RELEASE"; then
  echo "ERROR: release still present after uninstall" >&2
  exit 1
fi

# ---------- 3. Wait for leftover pods to terminate ----------
if kubectl get ns "$NS" >/dev/null 2>&1; then
  log "Waiting for pods to terminate..."
  kubectl -n "$NS" wait --for=delete pod \
    -l "app.kubernetes.io/instance=$RELEASE" --timeout=120s 2>/dev/null || true
fi
#echo "end of cleanup"

# test case 3 TLS ingress 
# below works as expected using local Kind cluster 
# echo "helmp upgrade dolphin-operator"
# ---------- 6. Reinstall ----------
log "Updating repos..."
helm repo update

log "Installing $RELEASE..."
helm install "$RELEASE" "$CHART" \
  -n "$NS" --create-namespace \
  --wait --timeout "$TIMEOUT" --atomic

# ---------- 7. Verify ----------
log "Verifying install..."
helm -n "$NS" status "$RELEASE"
kubectl -n "$NS" rollout status deploy \
  -l "app.kubernetes.io/instance=$RELEASE" --timeout="$TIMEOUT"
kubectl -n "$NS" get pods

log "Done."
# sleep 5
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
#kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress.yaml
kubectl apply -f - <<EOF 
# TLS ingress example, requires the below two applications
# https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml
# https://github.com/GoogleCloudPlatform/microservices-demo
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: tls-ingress
  namespace: dolphin
spec:
  ingressClassName: dolphin
  rules:
  - host: bookinfo.cilium.rocks
    http:
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
  tls:
  - hosts:
    - bookinfo.cilium.rocks
    secretName: tls-ingress-secret
---
apiVersion: v1
kind: Endpoints
metadata:
  name: dolphin-ingress-tls-ingress
  namespace: dolphin
subsets:
- addresses:
  - ip: 192.192.192.192
  ports:
  - port: 9999
    protocol: TCP
EOF
 
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
#echo "apply cilium envoy configure  for tls-ingress routing and TLS termination"
#kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress-envoyconfig.yaml

# --- In-cluster cert-validated verification via busybox pod ---
# curl -k https://bookinfo.cilium.rocks/details/1 --resolve bookinfo.cilium.rocks:443:{TLS INGRESS VIP} -v 
# {"id":1,"author":"William Shakespeare","year":1595,"type":"paperback","pages":200,"publisher":"PublisherA","language":"English","ISBN-10":"1234567890","ISBN-13":"123-1234567890"}
kubectl -n dolphin delete pod busybox --ignore-not-found --wait=true

echo "Taking a look at the pods status on the GH Actions cluster"

kubectl -n dolphin get pods -o wide || true
kubectl -n dolphin get pods -l app=details -o yaml 2>/dev/null \
  | grep -A5 -E "phase|reason|message" || true
kubectl -n dolphin describe pod -l app=details || true
kubectl get events -n dolphin --sort-by='.lastTimestamp' | tail -40 || true
kubectl top nodes 2>/dev/null || echo "metrics-server not installed"
kubectl describe nodes | grep -A5 -E "Conditions:|Allocated resources" || true

echo "ensuring bookinfo backends are present and healthy before TLS test"
kubectl -n dolphin apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

kubectl -n dolphin rollout status deployment/details-v1 --timeout=120s || exit 1
kubectl -n dolphin rollout status deployment/productpage-v1 --timeout=120s || exit 1

wait_for_endpoints dolphin details || exit 1
wait_for_endpoints dolphin productpage || exit 1

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

set -x

echo "Running:"
printf 'kubectl -n "%s" exec "%s" -- curl -sSL -o /tmp/response.json -w "%%{http_code}" --resolve "%s:443:%s" --cacert "%s" "%s"\n' \
    "${NAMESPACE}" \
    "${POD}" \
    "${HOST}" \
    "${tlsingressip}" \
    "${CACERT}" \
    "${URL}"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:443:${tlsingressip}" \
  --cacert "${CACERT}" \
  "${URL}"; then
    echo "TLS ingress verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi

##
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
# echo "TLS ingress verification succeeded"

# we could verify migrated tls-ingress to tls-gateway and gateway access works as expected
# .github/actions/tests/kindenv/ingressintegrationtests_setup/tlsingress-migratetogatewayapi
# apply gateway and httproute, apply ciliumenvoyconfig for tls-gateway
# verfiication similar as above TLS-ingress, only need tls-gateway-externalIP
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/details/1
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/productpage

# --- Cleanup ---
# kubectl -n dolphin delete pod netshoot --ignore-not-found
# kubectl -n dolphin delete configmap bookinfo-ca --ignore-not-found
# kubectl -n cilium-secrets delete secret tls-ingress-secret --ignore-not-found
# kubectl delete -f .github/actions/tests/kindenv/ingressintegrationtests_setup/ingress-conformance/dolphin-tls-ingress-envoyconfig.yaml --ignore-not-found
# cd ..
# rm -rf mkcert