#!/usr/bin/env bash
#
# Library: verifies that a dolphin Ingress is really programmed in the cilium data plane.
# Source it, set the IC_* variables you want to override, then call:
#
#     IC_INGRESS=basic-ingress IC_BACKEND_SERVICES="details productpage"
#     ingressip=$(ic_check_ingress_ready) || exit 1
#
# On success the Ingress address is printed on stdout (everything else goes to stderr).
#
# The stages, in the order things have to happen:
#   1. operator output : CiliumEnvoyConfig exists and is sane (routes -> clusters)
#   2. backends        : the Services behind the routes have ready endpoints
#   3. Ingress address : the LB Service got its IP
#   4. cilium-agent    : on EVERY node the LB IP:port is redirected to the Envoy proxy
#   5. traffic         : N consecutive correct HTTP answers through the ingress
#
# 1-4 decide whether the route exists; curl (5) is only the final confirmation, so one
# flaky curl can no longer fail or pass the check.
#
# Notes
#  - works with bash 3.2+ (macOS) and with `set -Eeuo pipefail` in the caller
#  - all functions/variables are prefixed ic_ / IC_ so they don't clash with other helpers
#  - `jq` is optional; it enables the deeper CiliumEnvoyConfig checks
#  - ic_check_ingress_ready runs in a subshell: its EXIT trap and `exit` never touch the caller

ic_init_config() {
  IC_NAMESPACE="${IC_NAMESPACE:-dolphin}"
  IC_INGRESS="${IC_INGRESS:-basic-ingress}"
  IC_LB_SERVICE="${IC_LB_SERVICE:-dolphin-ingress-${IC_INGRESS}}"
  IC_CEC_NAME="${IC_CEC_NAME:-dolphin-ingress-${IC_NAMESPACE}-${IC_INGRESS}}"   # dedicated mode
  IC_BACKEND_SERVICES="${IC_BACKEND_SERVICES:-details productpage}"       # in $IC_NAMESPACE
  IC_INGRESS_PORT="${IC_INGRESS_PORT:-80}"

  IC_URL_PATH="${IC_URL_PATH:-/details/1}"
  IC_EXPECT="${IC_EXPECT:-William Shakespeare}"

  IC_TIMEOUT="${IC_TIMEOUT:-120}"                       # seconds, per stage
  IC_INTERVAL="${IC_INTERVAL:-5}"                       # seconds between attempts
  IC_PROBE_INTERVAL="${IC_PROBE_INTERVAL:-2}"           # seconds between HTTP probes
  IC_STABLE_SUCCESSES="${IC_STABLE_SUCCESSES:-3}"       # consecutive good answers required

  IC_CILIUM_NS="${IC_CILIUM_NS:-kube-system}"
  IC_CILIUM_SELECTOR="${IC_CILIUM_SELECTOR:-k8s-app=cilium}"
  IC_CILIUM_CONTAINER="${IC_CILIUM_CONTAINER:-cilium-agent}"

  IC_PROBE_IMAGE="${IC_PROBE_IMAGE:-curlimages/curl}"
  IC_PROBE_POD="ingress-probe-$$"

  IC_CEC_RES="ciliumenvoyconfigs.cilium.io"
  IC_DEC_RES="dolphinenvoyconfigs.dolphin.io"

  IC_ADDR=""
}

ic_log()  { echo "[$(date +%H:%M:%S)] $*" >&2; }
ic_have() { command -v "$1" >/dev/null 2>&1; }

ic_cleanup() {
  kubectl -n "$IC_NAMESPACE" delete pod "$IC_PROBE_POD" --ignore-not-found --wait=false >/dev/null 2>&1 || true
}

# ic_retry <description> <command...> : runs the command every IC_INTERVAL until it succeeds or IC_TIMEOUT
ic_retry() {
  local what=$1
  shift
  local deadline=$((SECONDS + IC_TIMEOUT))

  while ((SECONDS < deadline)); do
    if "$@"; then
      return 0
    fi
    ic_log "  ... waiting for: $what"
    sleep "$IC_INTERVAL"
  done

  ic_log "ERROR: timeout waiting for: $what"
  return 1
}

# ---------------------------------------------------------------------------
# 1. CiliumEnvoyConfig produced by the operator
# ---------------------------------------------------------------------------

ic_cec_json() {
  kubectl -n "$IC_NAMESPACE" get "$IC_CEC_RES" "$IC_CEC_NAME" -o json 2>/dev/null
}

# Returns 0 only when the CEC is complete and internally consistent.
ic_cec_is_valid() {
  local json problems=0
  json=$(ic_cec_json) || { ic_log "  CiliumEnvoyConfig $IC_NAMESPACE/$IC_CEC_NAME not found yet"; return 1; }

  # things the data plane must never get (see generated-cec.yaml)
  if grep -q 'proxy_protocol' <<<"$json"; then
    ic_log "  CEC contains the proxy_protocol listener filter (cilium-envoy would reset plain HTTP)"
    problems=1
  fi
  if grep -q '"connectTimeout"' <<<"$json"; then
    ic_log "  note: CEC sets connectTimeout on a cluster (harmless, not expected)"
  fi

  if ic_have jq; then
    local listeners routes clusters has_lb dangling

    listeners=$(jq '[.spec.resources[]? | select(.["@type"] | endswith("listener.v3.Listener"))] | length' <<<"$json")
    routes=$(jq '[.spec.resources[]? | select(.["@type"] | endswith("route.v3.RouteConfiguration"))] | length' <<<"$json")
    clusters=$(jq '[.spec.resources[]? | select(.["@type"] | endswith("cluster.v3.Cluster"))] | length' <<<"$json")
    if [[ "$listeners" -lt 1 || "$routes" -lt 1 || "$clusters" -lt 1 ]]; then
      ic_log "  CEC incomplete: listeners=$listeners routeConfigs=$routes clusters=$clusters"
      problems=1
    fi

    # the CEC must be bound to the Ingress' LB Service on the ingress port
    has_lb=$(jq --arg lb "$IC_LB_SERVICE" --argjson port "$IC_INGRESS_PORT" \
      '[.spec.services[]? | select(.name == $lb and ((.ports // []) | index($port)) != null)] | length' <<<"$json")
    if [[ "$has_lb" -lt 1 ]]; then
      ic_log "  CEC.spec.services has no entry for $IC_LB_SERVICE on port $IC_INGRESS_PORT"
      problems=1
    fi

    # every cluster a route points at has to exist, otherwise Envoy rejects/503s the route
    dangling=$(jq -r '
      ([.spec.resources[]? | select(.["@type"] | endswith("cluster.v3.Cluster")) | .name]) as $known
      | [ .spec.resources[]?
          | select(.["@type"] | endswith("route.v3.RouteConfiguration"))
          | .virtualHosts[]?.routes[]?.route?
          | (.cluster // empty), (.weightedClusters.clusters[]?.name // empty) ]
      | unique | map(select(. as $c | ($known | any(. == $c)) | not)) | .[]' <<<"$json")
    if [[ -n "$dangling" ]]; then
      ic_log "  routes reference clusters that do not exist: $(tr '\n' ' ' <<<"$dangling")"
      problems=1
    fi
  else
    ic_log "  (jq not installed: skipping route/cluster consistency checks)"
  fi

  ((problems == 0))
}

# ---------------------------------------------------------------------------
# 2. backends
# ---------------------------------------------------------------------------

ic_backend_has_ready_endpoint() {
  local svc=$1 addrs
  addrs=$(kubectl -n "$IC_NAMESPACE" get endpointslices -l "kubernetes.io/service-name=${svc}" \
    -o jsonpath='{range .items[*]}{range .endpoints[?(@.conditions.ready==true)]}{.addresses[0]}{" "}{end}{end}' 2>/dev/null)
  [[ -n "${addrs// /}" ]]
}

ic_backends_ready() {
  local svc missing=""
  for svc in $IC_BACKEND_SERVICES; do
    ic_backend_has_ready_endpoint "$svc" || missing="$missing $svc"
  done
  if [[ -n "$missing" ]]; then
    ic_log "  no ready endpoints for:$missing"
    return 1
  fi
}

# ---------------------------------------------------------------------------
# 3. Ingress address (sets IC_ADDR)
# ---------------------------------------------------------------------------

ic_ingress_has_address() {
  IC_ADDR=$(kubectl -n "$IC_NAMESPACE" get ingress "$IC_INGRESS" \
    -o jsonpath='{.status.loadBalancer.ingress[0].ip}{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null)
  [[ -n "$IC_ADDR" ]]
}

# ---------------------------------------------------------------------------
# 4. cilium-agent: LB IP:port redirected to the Envoy proxy on every node
# ---------------------------------------------------------------------------

# Without the redirect the LB IP still points at the dummy backend
# (192.192.192.192:9999) and connections are reset, whatever the CEC says.
ic_redirect_on_all_nodes() {
  local addr=$1 pods pod line notready=""

  pods=$(kubectl -n "$IC_CILIUM_NS" get pods -l "$IC_CILIUM_SELECTOR" \
    -o jsonpath='{range .items[*]}{.metadata.name}{" "}{end}' 2>/dev/null)
  if [[ -z "${pods// /}" ]]; then
    ic_log "  no cilium agent pods found ($IC_CILIUM_NS, $IC_CILIUM_SELECTOR)"
    return 1
  fi

  for pod in $pods; do
    line=$(kubectl -n "$IC_CILIUM_NS" exec "$pod" -c "$IC_CILIUM_CONTAINER" -- cilium-dbg service list 2>/dev/null |
      grep -F "${addr}:${IC_INGRESS_PORT}/TCP" | grep -E '127\.0\.0\.1:[0-9]+|L7' || true)
    if [[ -z "$line" ]]; then
      notready="$notready $pod"
    fi
  done

  if [[ -n "$notready" ]]; then
    ic_log "  proxy redirect for ${addr}:${IC_INGRESS_PORT} not programmed yet on:$notready"
    return 1
  fi
}

# ---------------------------------------------------------------------------
# 5. traffic: final confirmation through a long-lived probe pod
# ---------------------------------------------------------------------------

# One pod + `kubectl exec` instead of a new pod per attempt: no image pull / attach
# race per probe, and exec output is reliable.
ic_start_probe_pod() {
  kubectl -n "$IC_NAMESPACE" delete pod "$IC_PROBE_POD" --ignore-not-found --wait=true >/dev/null 2>&1 || true
  if ! kubectl -n "$IC_NAMESPACE" run "$IC_PROBE_POD" --restart=Never --image="$IC_PROBE_IMAGE" \
    --command -- sleep 3600 >/dev/null 2>&1; then
    ic_log "ERROR: cannot create probe pod"
    return 1
  fi
  if ! kubectl -n "$IC_NAMESPACE" wait --for=condition=Ready "pod/$IC_PROBE_POD" --timeout=120s >/dev/null 2>&1; then
    ic_log "ERROR: probe pod did not become Ready"
    return 1
  fi
}

# prints: HTTP_CODE:<code>, CURL_EXIT:<rc>, then the body
ic_probe_once() {
  local url=$1
  kubectl -n "$IC_NAMESPACE" exec "$IC_PROBE_POD" -- sh -c \
    "curl -sS --connect-timeout 3 --max-time 10 -o /tmp/body -w 'HTTP_CODE:%{http_code}\n' '$url' 2>&1; echo \"CURL_EXIT:\$?\"; cat /tmp/body 2>/dev/null" 2>&1
}

# IC_STABLE_SUCCESSES correct answers in a row; any failure resets the streak.
ic_wait_for_stable_route() {
  local addr=$1
  local url="http://${addr}:${IC_INGRESS_PORT}${IC_URL_PATH}"
  if [[ "$IC_INGRESS_PORT" == "80" ]]; then
    url="http://${addr}${IC_URL_PATH}"
  fi
  local deadline=$((SECONDS + IC_TIMEOUT))
  local streak=0 out="" reason=""

  while ((SECONDS < deadline)); do
    out=$(ic_probe_once "$url" || true)

    if grep -q 'HTTP_CODE:200' <<<"$out" && grep -qF "$IC_EXPECT" <<<"$out"; then
      streak=$((streak + 1))
      ic_log "  GET $url ok ($streak/$IC_STABLE_SUCCESSES)"
      if ((streak >= IC_STABLE_SUCCESSES)); then
        return 0
      fi
    else
      streak=0
      reason=$(grep -E '^(curl: |CURL_EXIT:|HTTP_CODE:)' <<<"$out" | tr '\n' ' ')
      ic_log "  GET $url failed, streak reset: ${reason:-no output}"
    fi
    sleep "$IC_PROBE_INTERVAL"
  done

  ic_log "ERROR: no $IC_STABLE_SUCCESSES consecutive good answers from $url within ${IC_TIMEOUT}s. Last output:"
  printf '%s\n' "$out" >&2
  return 1
}

# ---------------------------------------------------------------------------

ic_diagnostics() {
  ic_log "---- diagnostics ----"
  if ic_have jq; then
    ic_log "who last wrote the CiliumEnvoyConfig:"
    kubectl -n "$IC_NAMESPACE" get "$IC_CEC_RES" "$IC_CEC_NAME" --show-managed-fields -o json 2>/dev/null |
      jq -r '.metadata.managedFields[]? | "  \(.manager) \(.operation) \(.time)"' >&2 || true
  fi
  ic_log "Ingress / LB service:"
  kubectl -n "$IC_NAMESPACE" get ingress "$IC_INGRESS" >&2 2>&1 || true
  kubectl -n "$IC_NAMESPACE" get svc "$IC_LB_SERVICE" >&2 2>&1 || true
  ic_log "---------------------"
}

ic_fail() {
  ic_log "FAILED: $*"
  ic_diagnostics
  exit 1
}

ic_check_ingress_ready() (
  # Subshell on purpose (see header). Errors are handled explicitly, so errexit is off here.
  set +e
  ic_init_config
  trap ic_cleanup EXIT

  ic_log "1/5 CiliumEnvoyConfig $IC_NAMESPACE/$IC_CEC_NAME"
  ic_retry "CiliumEnvoyConfig to exist and be valid" ic_cec_is_valid || ic_fail "CiliumEnvoyConfig is missing or invalid"
  if kubectl -n "$IC_NAMESPACE" get "$IC_DEC_RES" "$IC_CEC_NAME" >/dev/null 2>&1; then
    ic_log "  (DolphinEnvoyConfig $IC_CEC_NAME present)"
  fi

  ic_log "2/5 backends: $IC_BACKEND_SERVICES"
  ic_retry "ready endpoints for the backend Services" ic_backends_ready || ic_fail "backends have no ready endpoints"

  ic_log "3/5 Ingress address"
  ic_retry "Ingress address" ic_ingress_has_address || ic_fail "Ingress $IC_NAMESPACE/$IC_INGRESS has no address"
  ic_log "  address: $IC_ADDR"

  ic_log "4/5 cilium-agent redirect for ${IC_ADDR}:${IC_INGRESS_PORT} (all nodes)"
  ic_retry "proxy redirect on every cilium agent" ic_redirect_on_all_nodes "$IC_ADDR" || ic_fail "cilium did not program the redirect"

  ic_log "5/5 traffic through the ingress"
  ic_start_probe_pod || ic_fail "probe pod"
  ic_wait_for_stable_route "$IC_ADDR" || ic_fail "ingress answers are not stable"

  ic_log "OK: CiliumEnvoyConfig is applied and the route answers consistently."
  printf '%s\n' "$IC_ADDR"
)
