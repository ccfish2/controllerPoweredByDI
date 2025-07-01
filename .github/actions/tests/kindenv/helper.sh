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