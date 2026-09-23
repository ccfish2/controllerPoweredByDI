#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source ".github/actions/tests/kindenv/gatewayapi_setup.sh"
source ".github/actions/tests/kindenv/lib/helper.sh"
source ".github/actions/tests/kindenv/lib/metallb.sh"
#!/usr/bin/env bash
set -euo pipefail

echo "Deploy Echo Server"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tlsbackendpolicy/echo-server.yaml
wait_for_endpoints dolphin backend || exit 1

echo "Deploy Gateway and Http Route"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tlsbackendpolicy/gwhttproute.yaml

# verify gatewayapi through l7 service connection
gatewayip=""
end=$((SECONDS+120))
while true; do
    gatewayip=$(kubectl -n dolphin get gateway my-gateway -o jsonpath="{.status.addresses[?(@.type=='IPAddress')].value}")

    if [[ -n "$gatewayip" ]]; then
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

echo "Deploy Cilium Envoy Config"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tlsbackendpolicy/cec80.yaml

echo "Verify the service using curl "
# curl -s --resolve "www.example.com:80:192.168.56.203" http://www.example.com:80/get
NAMESPACE="dolphin"
POD="netshoot"
HOST="www.example.com"
CACERT="/certs/${HOST}.pem"
URL="http://www.example.com/get"
echo "Running Test:"
printf 'kubectl -n "%s" exec "%s" -- curl -sSL -o /tmp/response.json -w "%%{http_code}" --resolve "%s:80:%s" "%s"\n' \
    "${NAMESPACE}" \
    "${POD}" \
    "${HOST}" \
    "${gatewayip}" \
    "${URL}"

if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:80:${gatewayip}" \
  "${URL}"; then
    echo "Backend SVC verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi

# {
#  "path": "/get",
#  "host": "www.example.com",
#  "method": "GET",
#  "proto": "HTTP/1.1",
#  "headers": {
#   "Accept": [
#    "*/*"
#   ],
#   "User-Agent": [
#    "curl/8.20.0"
#   ],
#   "X-Envoy-Internal": [
#    "true"
#   ],
#   "X-Forwarded-For": [
#    "172.19.0.1"
#   ],
#   "X-Forwarded-Proto": [
#    "http"
#   ],
#   "X-Request-Id": [
#    "bb913f3e-7538-4873-88e4-25499fe5b3ff"
#   ]
#  },
#  "namespace": "default",
#  "ingress": "",
#  "service": "",
#  "pod": "backend-86c6c76f-ptczl"
# }

echo "Setup TLS between Gateway and Backend Service"
echo "Install Certs using mkcert and generate tls secrets using the cert"
DOMAIN="www.example.com"
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
# for cilium
kubectl -n cilium-secrets delete secret example-cert --ignore-not-found
kubectl -n cilium-secrets create secret tls example-cert \
  --cert=$CERT_FILE \
  --key=$KEY_FILE
# for netshoot pod
kubectl -n dolphin delete secret example-cert --ignore-not-found
kubectl -n dolphin create secret tls example-cert \
  --cert=$CERT_FILE \
  --key=$KEY_FILE

kubectl -n dolphin create configmap example-ca \
  --from-file=ca.crt=$CERT_FILE \
  --dry-run=client -o yaml | kubectl apply -f -
# going back to parent folder
cd ..

echo "patch deployment with the certs"

kubectl -n dolphin patch deployment backend --type=json --patch '
- op: add
  path: /spec/template/spec/containers/0/volumeMounts
  value:
  - name: secret-volume
    mountPath: /etc/secret-volume
- op: add
  path: /spec/template/spec/volumes
  value:
  - name: secret-volume
    secret:
      secretName: example-cert
      items:
      - key: tls.crt
        path: crt
      - key: tls.key
        path: key
- op: add
  path: /spec/template/spec/containers/0/env/-
  value:
    name: TLS_SERVER_CERT
    value: /etc/secret-volume/crt
- op: add
  path: /spec/template/spec/containers/0/env/-
  value:
    name: TLS_SERVER_PRIVKEY
    value: /etc/secret-volume/key
'

echo "expose the service using 443"
kubectl -n dolphin apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  labels:
    app: backend
    service: backend
  name: backend
spec:
  selector:
    app: backend
  ports:
  - name: https
    port: 443
    protocol: TCP
    targetPort: 8443
EOF

echo "Deploy tls backend policy using the configmap"
kubectl -n dolphin apply -f .github/actions/tests/kindenv/gatewayapiv161/tlsbackendpolicy/tlsbackendpolicy.yaml

echo "patch httproute"
kubectl -n dolphin patch HTTPRoute backend --type=json --patch '
 - op: replace
   path: /spec/rules/0/backendRefs/0/port
   value: 443
'

echo "Patch Cilium Envoy Configuration"
kubectl apply -f .github/actions/tests/kindenv/gatewayapiv161/tlsbackendpolicy/cec443.yaml
sleep 30

#curl -vI --resolve "www.example.com:80:<YOUR_GATEWAY_EXTERNAL_IP>" http://www.example.com:80/get
if curl_with_retry "$NAMESPACE" "$POD" 90 5 \
  curl -sSL -o /tmp/response.json -w "%{http_code}" \
  --resolve "${HOST}:80:${gatewayip}" \
  "${URL}"; then
    echo "Backend TLS Policy verification succeeded (HTTP 200)"
    kubectl -n "${NAMESPACE}" exec "${POD}" -- cat /tmp/response.json
    echo
else
    echo "failed"
    exit 1
fi
# {
#  "path": "/get",
#  "host": "www.example.com",
#  "method": "GET",
#  "proto": "HTTP/1.1",
#  "headers": {
#   "Accept": [
#    "*/*"
#   ],
#   "User-Agent": [
#    "curl/7.81.0"
#   ],
#   "X-Envoy-Internal": [
#    "true"
#   ],
#   "X-Forwarded-For": [
#    "172.16.107.129"
#   ],
#   "X-Forwarded-Proto": [
#    "http"
#   ],
#   "X-Request-Id": [
#    "73c9a6d9-2561-45de-ac91-73f45290696e"
#   ]
#  },
#  "namespace": "default",
#  "ingress": "",
#  "service": "",
#  "pod": "backend-6bcff5cb64-xn9md",
#  "tls": {
#   "version": "TLSv1.3",
#   "serverName": "www.example.com",
#   "negotiatedProtocol": "http/1.1",
#   "cipherSuite": "TLS_AES_128_GCM_SHA256"
#  }
# }