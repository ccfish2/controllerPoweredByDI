#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

source "${SCRIPT_DIR}/gatewayapi_setup.sh"
source "${SCRIPT_DIR}/lib/helper.sh"
source "${SCRIPT_DIR}/lib/metallb.sh"

NAMESPACE="dolphin"
GATEWAY_CLASS="dolphin"

echo "Deploy test applications"
kubectl -n "${NAMESPACE}" apply -f https://raw.githubusercontent.com/istio/istio/release-1.11/samples/bookinfo/platform/kube/bookinfo.yaml

echo "Deploy Cilium CRDS"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive || true # cilium CRDs
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-agent.yaml
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-envoy.yaml

echo "Verify echo pods are up and running"
NAMESPACE="dolphin"
TIMEOUT=120
INTERVAL=5
wait_for_pods "app=echo-1" "echo-1 pod" || exit 1
wait_for_pods "app=echo-2" "echo-2 pod" || exit 1
echo "✓ Echo pods are running"

echo "Deploy Cilium CRDS"
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/crds/ --recursive || true # cilium CRDs
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-agent.yaml
kubectl apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/custom-envoy.yaml

echo "Checking kube-system cilium agent and envoy pods are ready"
NAMESPACE="kube-system"
TIMEOUT=120
INTERVAL=5
wait_for_pods "k8s-app=cilium" "cilium agent" || exit 1
wait_for_pods "k8s-app=cilium-envoy" "cilium-envoy" || exit 1


echo "Deploy GatewayClass, DolphinGatewayClassConfig, Gateway, HTTPRoute"
kubectl -n dolphin apply -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/parameterized-gatewayclass/deploy.yaml
sleep 180

#!/usr/bin/env bash
set -euo pipefail
echo "Deploy Cilium Envoy Cnofig for Gateway and HTTP Route"
kubectl -n dolphin -f .github/actions/tests/kindenv/ingressintegrationtests_setup/gatewayapi/parameterized-gatewayclass/nodeport-gateway-cec.yaml
sleep 10

echo "Deploying netshoot client"
# grpcurl installed alongside curl/openssl in the netshoot image.
kubectl -n "${NAMESPACE}" delete pod netshoot --ignore-not-found --wait=true
kubectl -n "${NAMESPACE}" apply -f - <<EOF
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
EOF
kubectl -n "${NAMESPACE}" wait --for=condition=Ready pod/netshoot --timeout=120s


# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

NAMESPACE="${NAMESPACE:-dolphin}"
GATEWAY_SVC_NAME="dolphin-gateway-nodeport-gateway"
ENDPOINT_PATH="/details/1"

echo "==========================================="
echo "Testing NodePort Gateway Access"
echo "==========================================="

# 1. Get the NodePort
echo -e "\n${YELLOW}[1] Fetching gateway NodePort...${NC}"
NODE_PORT=$(kubectl -n "${NAMESPACE}" get svc "${GATEWAY_SVC_NAME}" -o jsonpath='{.spec.ports[0].nodePort}')
if [ -z "$NODE_PORT" ]; then
    echo -e "${RED}[ERROR] Failed to get NodePort${NC}"
    exit 1
fi
echo -e "${GREEN}✓ NodePort: ${NODE_PORT}${NC}"

# 2. Get the gateway ClusterIP
echo -e "\n${YELLOW}[4] Fetching gateway ClusterIP...${NC}"
CLUSTER_IP=$(kubectl -n "${NAMESPACE}" get svc "${GATEWAY_SVC_NAME}" -o jsonpath='{.spec.clusterIP}')
if [ -z "$CLUSTER_IP" ]; then
    echo -e "${RED}[ERROR] Failed to get ClusterIP${NC}"
    exit 1
fi
echo -e "${GREEN}✓ ClusterIP: ${CLUSTER_IP}${NC}"

# 3. Test via ClusterIP (from netshoot pod)
echo -e "\n${YELLOW}[6] Testing access via ClusterIP (${CLUSTER_IP}:80)...${NC}"
if kubectl -n "${NAMESPACE}" exec netshoot -- curl -s -f "http://${CLUSTER_IP}:80${ENDPOINT_PATH}" > /tmp/response_clusterip.json 2>&1; then
    echo -e "${GREEN}✓ ClusterIP access SUCCESS${NC}"
    echo "Response:"
    cat /tmp/response_clusterip.json
else
    echo -e "${RED}✗ ClusterIP access FAILED${NC}"
    exit 1
fi

# 4. Find the node where details pod is running
echo -e "\n${YELLOW}[2] Finding node running details pod...${NC}"
DETAILS_POD=$(kubectl -n "${NAMESPACE}" get pods -l app=details -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$DETAILS_POD" ]; then
    echo -e "${RED}[ERROR] Failed to find details pod${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Details pod: ${DETAILS_POD}${NC}"
 
# Get the node where details pod is running
DETAILS_NODE_NAME=$(kubectl -n "${NAMESPACE}" get pod "${DETAILS_POD}" -o jsonpath='{.spec.nodeName}')
if [ -z "$DETAILS_NODE_NAME" ]; then
    echo -e "${RED}[ERROR] Failed to find node for details pod${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Node name: ${DETAILS_NODE_NAME}${NC}"
 
# Get the InternalIP of that node
DETAILS_NODE_IP=$(kubectl get node "${DETAILS_NODE_NAME}" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')
if [ -z "$DETAILS_NODE_IP" ]; then
    echo -e "${RED}[ERROR] Failed to get IP for node ${DETAILS_NODE_NAME}${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Node IP: ${DETAILS_NODE_IP}${NC}"

# 5. Test via NodePort on the details node
echo -e "\n${YELLOW}[6] Testing access via NodePort (${DETAILS_NODE_NAME} @ ${DETAILS_NODE_IP}:${NODE_PORT})...${NC}"
if kubectl -n "${NAMESPACE}" exec netshoot -- curl -s -f "http://${DETAILS_NODE_IP}:${NODE_PORT}${ENDPOINT_PATH}" > /tmp/response_nodeport_details_node.json 2>&1; then
    echo -e "${GREEN}✓ NodePort (Details Node) access SUCCESS${NC}"
    echo "Response:"
    cat /tmp/response_nodeport_details_node.json
else
    echo -e "${RED}✗ NodePort (Details Node) access FAILED${NC}"
    exit 1
fi

echo -e "\n${GREEN}==========================================="
echo "All tests PASSED! ✓"
echo "===========================================${NC}"
exit 0