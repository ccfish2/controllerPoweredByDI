#!/usr/bin/env bash
# Set the namespace
NAMESPACE="dolphin"

# Function to check PVC status
check_pvc_status() {
    # Get the PVC status for the given namespace
    PVC_STATUS=$(kubectl get pvc -n "$NAMESPACE" --no-headers -o custom-columns=":status.phase")

    # Check if all PVCs are in 'Bound' status
    for status in $PVC_STATUS; do
        if [[ "$status" != "Bound" ]]; then
            return 1
        fi
    done

    # If all PVCs are in 'Bound' status
    return 0
}

# Check PVC status
if check_pvc_status; then
    echo "All PVCs are in 'Bound' status."
    exit 0
else
    echo "Some PVCs are not in 'Bound' status. Waiting for 2 minutes..."
    sleep 120
    # Re-check PVC status after 2 minutes
    if check_pvc_status; then
        echo "All PVCs are now in 'Bound' status."
        exit 0
    else
        echo "PVCs are still not in 'Bound' status after waiting."
        exit 1
    fi
fi