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