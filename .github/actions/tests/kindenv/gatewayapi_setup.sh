#!/usr/bin/env bash
set -euo pipefail

wait_for_gatewayclass_accepted() {
  local gatewayclass_name="$1"
  local timeout="${2:-60}"   # default timeout 60 seconds
  local interval="${3:-2}"   # default polling interval 2 seconds
  local elapsed=0

  echo "⏳ Waiting for GatewayClass '$gatewayclass_name' to be accepted..."

  while true; do
    local status
    status=$(kubectl get gatewayclass "$gatewayclass_name" -o jsonpath="{.status.conditions[?(@.type=='Accepted')].status}" 2>/dev/null || echo "")

    if [[ "$status" == "True" ]]; then
      echo "✅ GatewayClass '$gatewayclass_name' is accepted."
      return 0
    fi

    if (( elapsed >= timeout )); then
      echo "❌ Timed out waiting for GatewayClass '$gatewayclass_name' to be accepted."
      return 1
    fi

    sleep "$interval"
    ((elapsed += interval))
  done
}

wait_for_httproute_ready() {
  local namespace="$1"
  local route_name="$2"
  local timeout="${3:-60}"   # default timeout 60 seconds
  local interval="${4:-2}"   # default polling interval 2 seconds
  local elapsed=0

  echo "⏳ Waiting for HTTPRoute '$namespace/$route_name' to be Accepted and ResolvedRefs..."

  while true; do
    local json
    json=$(kubectl get httproute "$route_name" -n "$namespace" -o json 2>/dev/null || echo "")

    if [[ -z "$json" ]]; then
      echo "❌ HTTPRoute '$namespace/$route_name' not found."
      return 1
    fi

    # Use jq to check:
    local match
    match=$(jq -r '
      .status.parents[]? |
      select(
        .parentRef.group == "gateway.networking.k8s.io" and
        .parentRef.kind == "Gateway" and
        .parentRef.name == "tls-gateway"
      ) |
      select(
        any(.conditions[]; .type == "Accepted" and .status == "True") and
        any(.conditions[]; .type == "ResolvedRefs" and .status == "True")
      )
    ' <<< "$json")

    if [[ -n "$match" ]]; then
      echo "✅ HTTPRoute '$namespace/$route_name' is Accepted and ResolvedRefs=True under correct parentRef."
      return 0
    fi

    if (( elapsed >= timeout )); then
      echo "❌ Timed out waiting for HTTPRoute '$namespace/$route_name' to be ready."
      return 1
    fi

    sleep "$interval"
    ((elapsed += interval))
  done
}

verify_gateway_ready() {
  local namespace="$1"
  local gateway_name="$2"
  local timeout="${3:-60}"
  local interval="${4:-2}"
  local elapsed=0

  echo "⏳ Verifying Gateway '$gateway_name' in namespace '$namespace'..."

  while true; do
    local output
    output=$(kubectl get gateway "$gateway_name" -n "$namespace" -o json 2>/dev/null)

    if [[ -z "$output" ]]; then
      echo "❌ Gateway '$gateway_name' not found."
      return 1
    fi

    local ip
    ip=$(echo "$output" | jq -r '.status.addresses[0].value // empty')

    local accepted
    accepted=$(echo "$output" | jq -r '.status.conditions[] | select(.type=="Accepted") | .status')

    local programmed
    programmed=$(echo "$output" | jq -r '.status.conditions[] | select(.type=="Programmed") | .status')

    local attached_routes
    attached_routes=$(echo "$output" | jq -r '.status.listeners[0].attachedRoutes // 0')

    local listener_conditions
    listener_conditions=$(echo "$output" | jq -r '.status.listeners[0].conditions[] | "\(.type)=\(.status)"')

    if [[ "$ip" != "" && "$accepted" == "True" && "$programmed" == "True" && "$attached_routes" -ge 1 ]] &&
       echo "$listener_conditions" | grep -q "Accepted=True" &&
       echo "$listener_conditions" | grep -q "Programmed=True" &&
       echo "$listener_conditions" | grep -q "ResolvedRefs=True"; then
      echo "✅ Gateway '$gateway_name' is fully configured and ready (IP: $ip)"
      return 0
    fi

    if (( elapsed >= timeout )); then
      echo "❌ Timed out waiting for Gateway '$gateway_name' to be ready."
      return 1
    fi

    sleep "$interval"
    ((elapsed += interval))
  done
}

verify_gateway_endpoints() {
  local namespace="${1:-dolphin}"
  local name="${2:-dolphin-gateway-tls-gateway}"
  local expected_ip="192.192.192.192"
  local expected_port=9999
  local expected_protocol="TCP"

  echo "🔍 Verifying Endpoints '$name' in namespace '$namespace'..."

  local output
  output=$(kubectl get endpoints "$name" -n "$namespace" -o json 2>/dev/null)

  if [[ -z "$output" ]]; then
    echo "❌ Endpoints '$name' not found."
    return 1
  fi

  local actual_ip actual_port actual_protocol
  actual_ip=$(echo "$output" | jq -r '.subsets[0].addresses[0].ip // empty')
  actual_port=$(echo "$output" | jq -r '.subsets[0].ports[0].port // empty')
  actual_protocol=$(echo "$output" | jq -r '.subsets[0].ports[0].protocol // empty')

  if [[ "$actual_ip" == "$expected_ip" && "$actual_port" == "$expected_port" && "$actual_protocol" == "$expected_protocol" ]]; then
    echo "✅ Endpoints '$name' match expected values."
    return 0
  else
    echo "❌ Endpoints '$name' do not match expected values."
    echo "    Expected: IP=$expected_ip, Port=$expected_port, Protocol=$expected_protocol"
    echo "    Found:    IP=$actual_ip, Port=$actual_port, Protocol=$actual_protocol"
    return 1
  fi
}

verify_gateway_tls_listener_ready() {
  local namespace="$1"
  local gateway_name="$2"
  local listener_name="${3:-https}"
  local timeout="${4:-60}"
  local interval="${5:-2}"
  local elapsed=0

  echo "⏳ Verifying Gateway listener '$listener_name' on '$gateway_name'..."

  while true; do
    local output
    output=$(kubectl get gateway "$gateway_name" -n "$namespace" -o json 2>/dev/null || echo "")

    if [[ -z "$output" ]]; then
      echo "❌ Gateway '$gateway_name' not found."
      return 1
    fi

    local listener
    listener=$(echo "$output" | jq -r --arg name "$listener_name" '.status.listeners[]? | select(.name == $name)')

    if [[ -n "$listener" ]]; then
      local accepted programmed resolved
      accepted=$(echo "$listener" | jq -r '.conditions[] | select(.type=="Accepted") | .status')
      programmed=$(echo "$listener" | jq -r '.conditions[] | select(.type=="Programmed") | .status')
      resolved=$(echo "$listener" | jq -r '.conditions[] | select(.type=="ResolvedRefs") | .status')

      if [[ "$accepted" == "True" && "$programmed" == "True" && "$resolved" == "True" ]]; then
        echo "✅ Gateway listener '$listener_name' is Accepted, Programmed, and ResolvedRefs=True."
        return 0
      fi
    fi

    if (( elapsed >= timeout )); then
      echo "❌ Timed out waiting for Gateway listener '$listener_name' to be ready."
      return 1
    fi

    sleep "$interval"
    ((elapsed += interval))
  done
}