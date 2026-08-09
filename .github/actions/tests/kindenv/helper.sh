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
  local service_name=$1
  local retries=10
  local sleep_seconds=5

  echo "🔍 Checking if service '$service_name' exists in namespace '$namespace'..."

  # Check if the service exists
  if ! kubectl get svc "$service_name" -n "$namespace" >/dev/null 2>&1; then
    echo "❌ Service '$service_name' not found in namespace '$namespace'."
    return 1
  fi

  echo "✅ Service exists. Waiting for EXTERNAL-IP to be assigned..."

  # Wait for EXTERNAL-IP
  for ((i=1; i<=retries; i++)); do
    external_ip=$(kubectl get svc "$service_name" -n "$namespace" -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    
    if [[ -n "$external_ip" ]]; then
      echo "✅ Service has external IP: $external_ip"
      return 0
    fi

    echo "⏳ Attempt $i/$retries: EXTERNAL-IP not assigned yet. Retrying in $sleep_seconds seconds..."
    sleep "$sleep_seconds"
  done

  echo "❌ EXTERNAL-IP was not assigned after $retries attempts."
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
      echo "Got 200"
      return 0
    fi
    echo "Got '$code', retrying... ($((end - SECONDS))s left)"
    sleep "$interval"
    if ((SECONDS > end)); then
      echo "ERROR: never got 200 within ${timeout}s (last: $code)"
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

wait_for_endpoints dolphin details || exit 1
wait_for_endpoints dolphin productpage || exit 1