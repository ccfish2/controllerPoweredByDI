#!/usr/bin/env bash
source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"

echo "Deploying a TLS-enabled gateway and validating HTTPS reconciliation"
 
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
echo "create tls secret that guard gateway httproutes"
kubectl create namespace cilium-secrets --dry-run=client -o yaml | kubectl apply -f -
kubectl -n cilium-secrets create secret tls ca \
  --cert=$CERT_FILE \
  --key=$KEY_FILE

# --- CA configmap for the in-cluster verification pod ---
echo "create configmap that persist the cacert for accessing service"
kubectl -n dolphin create configmap bookinfo-ca --from-file=bookinfo.cilium.rocks.pem=bookinfo.cilium.rocks.pem

cd ..

# --- Apply gateway and httproutes config ---
echo "deploy gateway api and http routes "
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/https-termination/gwhttps.yaml
 
#--- Wait for gatewayapi LoadBalancer IP VIP  ---
end=$((SECONDS + 120))
tlsgatewayip=""
while true; do
    tlsgatewayip=$(kubectl -n dolphin get gateway tls-gateway \
      -o jsonpath="{.status.addresses[0].value}" 2>/dev/null || true)
 
    if [[ -n "$tlsgatewayip" ]]; then
        echo "TLS Gateway Service IP acquired: $tlsgatewayip"
        break
    fi
 
    echo "Waiting for TLS Gateway IP..."
    sleep 5
 
    if ((SECONDS > end)); then
        echo "Timeout waiting for TLS Gateway LB IP"
        exit 1
    fi
done
 
# External connectivity check — confirm this helper does SNI/Host to $DOMAIN, not a hardcoded name
#verify_https_connectivity "$ingressip" "$DOMAIN" || exit 1
echo "apply cilium envoy configure  for tls-gateway routing and TLS termination"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/https-termination/cec-tls-termination.yaml

# --- In-cluster cert-validated verification via busybox pod ---
# curl -k https://bookinfo.cilium.rocks/details/1 --resolve bookinfo.cilium.rocks:443:{TLS INGRESS VIP} -v 
# {"id":1,"author":"William Shakespeare","year":1595,"type":"paperback","pages":200,"publisher":"PublisherA","language":"English","ISBN-10":"1234567890","ISBN-13":"123-1234567890"}
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

echo "Resolving ${HOST} -> ${tlsgatewayip}"
if [[ -z "${tlsgatewayip:-}" ]]; then
    echo "ERROR: tlsgatewayip is not set"
    exit 1
fi

set -x

echo "Running:"
printf 'kubectl -n "%s" exec "%s" -- curl -sSL -o /tmp/response.json -w "%%{http_code}" --resolve "%s:443:%s" --cacert "%s" "%s"\n' \
    "${NAMESPACE}" \
    "${POD}" \
    "${HOST}" \
    "${tlsgatewayip}" \
    "${CACERT}" \
    "${URL}"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:443:${tlsgatewayip}" \
  --cacert "${CACERT}" \
  "${URL}"; then
    echo "TLS ingress verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi

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
echo "TLS gateway verification succeeded"

# we could verify migrated tls-gateway to tls-gateway and gateway access works as expected
# .github/actions/tests/kindenv/gatewayintegrationtests_setup/tlsgateway-migratetogatewayapi
# apply gateway and httproute, apply ciliumenvoyconfig for tls-gateway
# verfiication similar as above TLS-gateway, only need tls-gateway-externalIP
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/details/1
# curl --resolve bookinfo.cilium.rocks:443:{tls-gateway-externalIP} --cacert bookinfo.cilium.rocks.pem -v https://bookinfo.cilium.rocks/productpage