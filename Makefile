SHELL :=/usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

ROOTDIR := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")
RELATIVE_PATH := $(shell echo $(realpath .) | sed "s;$(ROOT_DIR)[/]*;;")

PREFIX?=/usr
BINDIR?=$(PREFIX)/bin
CNIBINDIR?=/opt/cni/bin
CNICONFDIR?=/etc/cni/net.d
LIBDIR?=$(PREFIX)/lib
LOCALSTATEDIR?=/var
RUNDIR?=/var/run
CONFDIR?=/etc

export GO ?= go
NATIVE_ARCH = $(shell GOARCH=$(GO) env GOARCH)
export GOARCH ?= $(NATIVE_ARCH)

INSTALL = install

CONTAINER_ENGINE?=docker
DOCKER_FLAGS?=
DOCKER_BUILD_FLAGS?=

# minor diff between sed and gsed
SED ?= $(if $(shell command -v gsed),gsed,sed)

ifeq ($(DOCKER_DEV_ACCOUNT),)
  DOCKER_DEV_ACCOUNT=jimin1
endif

ifeq ($(DOCKER_IMG_TAG),)
  DOCKER_IMG_TAG=latest
endif

# 
DOCKER_BUILDER := default
export DOCKER_BUILDKIT=1
ifdef ARCH
  DOCKER_PLATFORMS := linux/arm64,linux/amd64
  DOCKER_BUILDER:= $(shell docker buildx ls | grep -E -e "[a-zA-Z0-9-]+ \*" | cut -d ' ' -f1)
  ifneq(,$(filter $(DOCKER_BUILDER), default, desk-linux))
    DOCKER_BUILDKIT_DRIVER :=
    ifdef DOCKER_BUILDKIT_IMAGE
      DOCKER_BUILDKIT_DRIVER := --driver docker-container --dirver-opt $(DOCKER_BUILDKIT_IMAGE)
    endif
    BUILDER_SETUP := $(shell docker buildx create --platform $(DOCKER_PLATFORMS) $(DOCKER_BUILDKIT_DRIVER) --use)
  # override default values
  ifneq ($(ARCH), multi)
    DOCKER_PLATFORMS := linux/$(ARCH)
  endif
  DOCKER_FLAGS += --push --platform $(DOCKER_PLATFORMS)
else
  ifeq ($(findstring --output,$(DOCKER_FLAGS)),)
    ifeq ($(findstring --push,$(DOCKER_FLAGS)),)
      DOCKER_FLAGS += load
    endif
  endif
endif
DOCKER_BUILDER := $(shell docker buildx ls | grep -E -e "[a-zA-Z0-9-]+ \*" | cut -d ' ' -f1)

##@ Docker Images
.PHONY: builder-info
builder-info: ## Print information about the docker builder that will be used for building images.
	@echo "Using Docker Buildx builder \"$(DOCKER_BUILDER)\" with build flags \"$(DOCKER_FLAGS)\"."

# genericc rule for .gitignore files
GIT_IGNORE_FILES := $(shell find . -not -path "./vendor*" -name .gitignore -print)
.PRECIOUS: %.dockerignore
%.dockerignore: $(GIT_IGNORE_FILES) Makefile.docker
	@-mkdir -p $(dir $@)
	@echo "/hack" > $@
	@echo ".git" >> $@
	@echo "/Makefile.docker" >> $@
	@echo $(dir $(GIT_IGNORE_FILES)) | tr ' ' '\n' | xargs -P1 -I {DIR} -n1 sed \
		-e '# Remove lines with white space, comments and files that must be passed to docker, "$$" due to make. #' \
			-e '/^[[:space:]]*$$/d' -e '/^#/d' -e '/GIT_VERSION/d' \
		-e '# Apply pattern in all directories if it contains no "/", keep "!" up front. #' \
			-e '/^[^!/][^/]*$$/s<^<**/<' -e '/^![^/]*$$/s<^!<!**/<' \
		-e '# Prepend with the directory name, keep "!" up front. #' \
			-e '/^[^!]/s<^<{DIR}<' -e '/^!/s<^!<!{DIR}<'\
		-e '# Remove leading "./", keep "!" up front. #' \
			-e 's<^\./<<' -e 's<^!\./<!<' \
		-e '# Append newline to the last line if missing. GNU sed does not do this automatically. #' \
			-e '$$a\' \
		{DIR}.gitignore >> $@

DOCKER_REGISTRY ?= hub.docker.com
ifeq ($(findstring /,$(DOCKER_DEV_ACCOUNT)),/)
    IMAGE_REPOSITORY := $(DOCKER_DEV_ACCOUNT)
else
    IMAGE_REPOSITORY := $(DOCKER_REGISTRY)/$(DOCKER_DEV_ACCOUNT)
endif

#
# Template for Docker images. Paramaters are:
# $(1) image target name
# $(2) Dockerfile path
# $(3) image name stem (e.g., DOLPHIN, DOLPHIN-operator, etc)
# $(4) image tag
# $(5) target
#
define DOCKER_IMAGE_TEMPLATE
.PHONY $(1)
$(1): GIT_VERSION $(2) $(2).gitignore GIT_VERSION builder-info 
  $(ECHO_DOCKER)$(2) $(IMAGE_REPOSITORY)/$(IMAGE_NAME)$$(UNSTRIPPED):$(4)
  $(eval IMAGE_NAME := $(subst %,$$$$*,$(3)))
ifeq ($(5),debug)
  @export NOSTRIP=1
endif
	$(QUIET) $(CONTAINER_ENGINE) buildx build -f $(subst %,$$*,$(2)) \
		$(DOCKER_BUILD_FLAGS) $(DOCKER_FLAGS) \
		$(if $(BASE_IMAGE),--build-arg BASE_IMAGE=$(BASE_IMAGE),) \
		--build-arg NOSTRIP=$${NOSTRIP} \
		--build-arg NOOPT=${NOOPT} \
		--build-arg LOCKDEBUG=${LOCKDEBUG} \
		--build-arg RACE=${RACE}\
		--build-arg V=${V} \
		--build-arg LIBNETWORK_PLUGIN=${LIBNETWORK_PLUGIN} \
		--build-arg DOLPHIN_SHA=$(firstword $(GIT_VERSION)) \
		--build-arg OPERATOR_VARIANT=$(IMAGE_NAME) \
		--build-arg DEBUG_HOLD=$(DEBUG_HOLD) \
		--target $(5) \
		-t $(IMAGE_REPOSITORY)/$(IMAGE_NAME)$${UNSTRIPPED}$(DOCKER_IMAGE_SUFFIX):$(4) .
ifneq ($(KIND_LOAD),)
	sleep 1
	kind load docker-image $(IMAGE_REPOSITORY)/$(IMAGE_NAME)$${UNSTRIPPED}$(DOCKER_IMAGE_SUFFIX):$(4)
else
  ifeq ($(findstring --push,$(DOCKER_FLAGS)),)
	@echo 'Define "DOCKER_FLAGS=--push" to push the build results.'
    else
	$(CONTAINER_ENGINE) buildx imagetools inspect $(IMAGE_REPOSITORY)/$(IMAGE_NAME)$${UNSTRIPPED}$(DOCKER_IMAGE_SUFFIX):$(4)
	@echo '^^^ Images pushed, multi-arch manifest should be above. ^^^'
    endif
endif

$(1)-unstripped: NOSTRIP=1
$(1)-unstripped: UNSTRIPPED=-unstripped
$(1)-unstripped: $(1)
	@echo
endef

# docker-operator-images.
# We eat the ending of "operator" in to the stem ('%') to allow this pattern
# to build also 'docker-operator-image', where the stem would be empty otherwise.
#$(eval $(call DOCKER_IMAGE_TEMPLATE,docker-opera%-image,images/Dockerfile,opera%,$(DOCKER_IMAGE_TAG),release))

REGISTRIES ?= docker.io/jimin1
PUSH ?= false

OUTPUT := "type=image"
ifeq ($(PUSH),true)
OUTPUT := "type=registry,push=true"
endif

PLATFORMS=linux/amd64,linux/arm64

all-images: lint operator-image

lint:
	scripts/lint.sh

.buildx_builder:
	# see https://github.com/docker/buildx/issues/308
	mkdir -p ../.buildx
	docker buildx create --platform $(PLATFORMS) --buildkitd-flags '--debug' > $@

operator-image: .buildx_builder
	ROOT_CONTEXT=true operator-dev images/operator $(PLATFORMS) $(OUTPUT) "$$(cat .buildx_builder)" $(REGISTRIES)