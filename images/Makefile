# SPDX-License-Identifier: Apache-2.0

REGISTRIES ?= docker.io/jimin1

PUSH ?= false

OUTPUT := "type=image"
ifeq ($(PUSH),true)
OUTPUT := "type=registry,push=true"
endif

PLATFORMS=linux/amd64,linux/arm64

all-images: operator-image

.buildx_builder:
	mkdir -p .buildx
	docker buildx create --platform $(PLATFORMS) --buildkitd-flags '--debug' > $@

operator-image: .buildx_builder
	ROOT_CONTEXT=true ./build-image.sh operator-dev images/operator $(PLATFORMS) $(OUTPUT) "$$(cat .buildx_builder)" $(REGISTRIES)