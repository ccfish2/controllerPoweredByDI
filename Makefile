ROOT_DIR := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")

include Makefile.defs

TARGETS := dolphin-operator dolphin-operator-generic dolphin-operator-aws dolphin-operator-azure
.PHONY: all $(TARGETS) clean install docker-operator-image-generic
build: $(TARGETS)

IMAGE_REPOSITORY ?= docker.io/jimin1/dolphin-operator-generic
IMAGE_TAG ?= latest

docker-operator-image-generic: dolphin-operator-generic
	@echo "Building Docker image for dolphin-operator-generic"
	docker buildx build --platform=linux/amd64,linux/arm64 \
		-t $(IMAGE_REPOSITORY):$(IMAGE_TAG) \
		-t $(IMAGE_REPOSITORY):latest \
		-f ./images/Dockerfile --push .

dolphin-operator-generic:
	@echo "Running go build -o dolphin-operator-generic with GO_TAGS_FLAGS=ipam_provider_operator"
	go build -tags "ipam_provider_operator" -o dolphin-operator-generic .

dolphin-operator: GO_TAGS_FLAGS+=ipam_provider_aws,ipam_provider_azure,ipam_provider_operator
dolphin-operator-generic: GO_TAGS_FLAGS+=ipam_provider_operator
dolphin-operator-aws: GO_TAGS_FLAGS+=ipam_provider_aws
dolphin-operator-azure: GO_TAGS_FLAGS+=ipam_provider_azure

$(TARGETS):
	@echo "Running go build -o $@ with GO_TAGS_FLAGS=$(GO_TAGS_FLAGS)"
	go build -race 	-tags "$(GO_TAGS_FLAGS)" -o $@ .

$(TARGET):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $@

clean:
	@$(ECHO_CLEAN)
	$(QUIET)rm -f $(TARGETS)
	$(GO) clean $(GOCLEAN)

install:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(foreach target,$(TARGETS), $(QUIET)$(INSTALL) -m 0755 $(target) $(DESTDIR)$(BINDIR);)

install-generic:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(QUIET)$(INSTALL) -m 0755 dolphin-operator-generic $(DESTDIR)$(BINDIR)

install-aws:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(QUIET)$(INSTALL) -m 0755 dolphin-operator-aws $(DESTDIR)$(BINDIR)

install-azure:
	$(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(QUIET)$(INSTALL) -m 0755 dolphin-operator-azure $(DESTDIR)$(BINDIR)

# Docker image build (from image/Makefile)
docker-image-generic:
	$(MAKE) -C image docker-image-generic

clean:
	@echo "Cleaning up"
	rm -f $(TARGETS)

install:
	$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
	$(foreach target,$(TARGETS), $(INSTALL) -m 0755 $(target) $(DESTDIR)$(BINDIR);)