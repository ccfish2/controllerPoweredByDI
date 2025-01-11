ROOT_DIR := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")

include Makefile.defs

TARGETS := dolphin-operator dolphin-operator-generic dolphin-operator-aws dolphin-operator-azure

.PHONY: all $(TARGETS) clean install

all: $(TARGETS)

dolphin-operator: GO_TAGS_FLAGS+=ipam_provider_aws,ipam_provider_azure,ipam_provider_operator
dolphin-operator-generic: GO_TAGS_FLAGS+=ipam_provider_operator
dolphin-operator-aws: GO_TAGS_FLAGS+=ipam_provider_aws
dolphin-operator-azure: GO_TAGS_FLAGS+=ipam_provider_azure

$(TARGETS):
	@$(ECHO_GO)
	$(QUIET)$(GO_BUILD) -o $(@)

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


kind-build-image-operator: ## Build dolphin-operator-dev docker image
	$(QUIET)$(MAKE) dev-docker-operator-generic-image$(DEBUGGER_SUFFIX) DOCKER_IMAGE_TAG=$(LOCAL_IMAGE_TAG)