
REGISTRIES ?= docker.io/jimin1

PUSH ?= false

OUTPUT := "type=image"
ifeq ($(PUSH),true)
OUTPUT := "type=registry,push=true"
endif

PLATFORMS=linux/amd64,linux/arm64

all-images: operator-image

operator-image: .buildx_builder
	ROOT_CONTEXT=true scripts/build-image.sh operator-dev images/ $(PLATFORMS) $(OUTPUT) "$$(cat .buildx_builder)" $(REGISTRIES)