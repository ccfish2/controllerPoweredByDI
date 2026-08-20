#!/usr/bin/env bash

verify_connectivity() {
  local ingress_ip="$1"
  local timeout=120
  local end=$((SECONDS + timeout))

  while true; do
    echo "Checking internal connectivity to basic-ingress service at $ingress_ip..."
    
    output=$(kubectl run busybox --rm -i --restart=Never --image=curlimages/curl -- \
      curl -s --fail -v http://$ingress_ip/details/1)

    if echo "$output" | grep -q "William Shakespeare"; then
      echo "Ingress LoadBalancer Service is reachable."
      return 0
    fi

    echo "Ingress LoadBalancer Service not reachable yet. Waiting 5 seconds..."
    sleep 5

    if (( SECONDS > end )); then
      echo "Timeout waiting for Ingress LoadBalancer Service to be reachable"
      return 1
    fi
  done
}

verify_https_connectivity() {
  local ingress_ip="$1"
  local hostname="${2:-example.com}"
  local timeout=120
  local end=$((SECONDS + timeout))

  while true; do
    echo "Checking HTTPS connectivity to $hostname at $ingress_ip..."

    output=$(kubectl run busybox --rm -i --restart=Never --image=curlimages/curl -- \
      curl -sS -k --resolve "$hostname:443:$ingress_ip" "https://$hostname/details/1")

    if echo "$output" | grep -q "William Shakespeare"; then
      echo "HTTPS ingress endpoint is reachable."
      return 0
    fi

    echo "HTTPS ingress endpoint not reachable yet. Waiting 5 seconds..."
    sleep 5

    if (( SECONDS > end )); then
      echo "Timeout waiting for HTTPS ingress endpoint to be reachable"
      return 1
    fi
  done
}

check_service_external_ip() {
  local namespace="dolphin"
  local service_name="${1:?Service name is required}"
  local timeout_seconds="${2:-120}"
  local interval_seconds="${3:-5}"
  local deadline=$((SECONDS + timeout_seconds))
  local external_ip

  echo "🔍 Waiting for service '$service_name' in namespace '$namespace'..."

  while (( SECONDS <= deadline )); do
    if ! kubectl get svc "$service_name" -n "$namespace" >/dev/null 2>&1; then
      echo "⏳ Service '$service_name' does not exist yet. Retrying in ${interval_seconds}s..."
      sleep "$interval_seconds"
      continue
    fi

    external_ip="$(
      kubectl get svc "$service_name" \
        -n "$namespace" \
        -o jsonpath='{.status.loadBalancer.ingress[0].ip}' \
        2>/dev/null
    )"

    if [[ -n "$external_ip" ]]; then
      echo "✅ Service '$service_name' has external IP: $external_ip"
      return 0
    fi

    echo "⏳ Service exists, but EXTERNAL-IP is not assigned yet. Retrying in ${interval_seconds}s..."
    sleep "$interval_seconds"
  done

  echo "❌ Timed out after ${timeout_seconds}s waiting for '$service_name' and its EXTERNAL-IP."
  kubectl get svc "$service_name" -n "$namespace" 2>/dev/null || true
  return 1
}

# Retries an in-cluster curl until it gets a 200, tolerating transient 503s
# while Envoy/xDS/Endpoints converge after a fresh Ingress/Gateway apply.
curl_with_retry() {
  local namespace=$1 pod=$2 timeout=${3:-90} interval=${4:-5}; shift 4
  local end=$((SECONDS + timeout))
  local code

  while true; do
    code=$(kubectl -n "$namespace" exec "$pod" -- "$@" 2>/dev/null) || code="curl_failed"
    if [[ "$code" == "200" ]]; then
      echo "Got 200" >&2
      return 0
    fi
    echo "Got '$code', retrying... ($((end - SECONDS))s left)" >&2
    sleep "$interval"
    if ((SECONDS > end)); then
      echo "ERROR: never got 200 within ${timeout}s (last: $code)" >&2
      kubectl -n "$namespace" exec "$pod" -- cat /tmp/response.json 2>/dev/null || true
      return 1
    fi
  done
}

wait_for_endpoints() {
  local ns=$1 svc=$2 timeout=${3:-90}
  local end=$((SECONDS + timeout))
  while true; do
    count=$(kubectl -n "$ns" get endpoints "$svc" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null | wc -w)
    if [[ "$count" -gt 0 ]]; then
      echo "$svc has $count endpoint(s)"
      return 0
    fi
    echo "Waiting for endpoints on $svc..."
    sleep 3
    if ((SECONDS > end)); then
      echo "Timeout waiting for endpoints on $svc"
      kubectl -n "$ns" get endpoints "$svc" -o yaml
      return 1
    fi
  done
}

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