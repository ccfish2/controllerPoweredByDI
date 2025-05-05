#!/usr/bin/env bash

# Wait for at least 2 Available PVs on different nodes
timeout=120  # 2 minutes
interval=5   # check every 5 seconds
elapsed=0

while [ $elapsed -lt $timeout ]; do
    # Get count of Available PVs per node
    current_count=$(kubectl get pv --no-headers | awk '/Available/ {print $0}' | wc -l)

    if [ "$current_count" -ge 2 ]; then
        echo "Found at least 2 Available PVs"
        exit 0
    else
        sleep $interval
        elapsed=$((elapsed + interval))
    fi
done

echo "Timeout reached. Did not find at least 2 Available PVs."
exit 1
