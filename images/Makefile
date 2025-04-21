
REGISTRIES ?= docker.io/jimin1

PUSH ?= false

OUTPUT := "type=image"
ifeq ($(PUSH),true)
OUTPUT := "type=registry,push=true"
endif

PLATFORMS=linux/amd64,linux/arm64

all-images: docker-image-generic

docker-image-generic:
	docker build \
		--build-arg TARGETOS=linux \
		--build-arg TARGETARCH=amd64 \
		--build-arg GO_TAGS_FLAGS="home_provider_generic" \
		-f Dockerfile \
		-t dolphin-operator-generic:latest \
		..

docker-image-%:
	docker build \
		--build-arg TARGETOS=linux \
		--build-arg TARGETARCH=amd64 \
		--build-arg GO_TAGS_FLAGS="home_provider_$*" \
		-f Dockerfile \
		-t dolphin-operator-$*:latest \
		..
