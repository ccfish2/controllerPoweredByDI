#!/usr/bin/env bash

# metalb
# kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.14.5/config/manifests/metallb-native.yaml


# # metallb-config.yaml
# apiVersion: metallb.io/v1beta1
# kind: IPAddressPool
# metadata:
#   name: custom-ip-pool
#   namespace: metallb-system
# spec:
#   addresses:
#   - CIDR range

# ---
# apiVersion: metallb.io/v1beta1
# kind: L2Advertisement
# metadata:
#   name: l2-advertisement
#   namespace: metallb-system


# kubectl deploy nginx --image=nginx 
# kubectl expose deployment nginx --port=80 --type=LoadBalancer