#!/usr/bin/env bash

# verify gateway is programmed 
wait_for_gateway_ready() {
  local namespace="$1"
  local gateway_name="$2"
  local timeout=120  # seconds
  local interval=5   # seconds
  local elapsed=0

  is_valid_ip() {
    [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] && return 0 || return 1
  }

  while (( elapsed < timeout )); do
    local line
    line=$(kubectl -n "$namespace" get gateway "$gateway_name" --no-headers 2>/dev/null)

    if [[ -z "$line" ]]; then
      echo "Gateway $gateway_name not found in namespace $namespace."
      return 1
    fi

    local address programmed
    address=$(echo "$line" | awk '{print $3}')
    programmed=$(echo "$line" | awk '{print $4}')

    if is_valid_ip "$address" && [[ "$programmed" == "True" ]]; then
      echo "Gateway $gateway_name is ready with valid IP $address."
      return 0
    fi

    sleep "$interval"
    ((elapsed+=interval))
  done

  echo "Timed out waiting for gateway $gateway_name to have a valid IP and be programmed."
  return 1
}