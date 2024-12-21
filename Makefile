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

ifeq ($(DOCKER_DEV_ACCNT),)
  DOCKER_DEV_ACCNT=jimin1
endif

ifeq ($(DOCKER_IMG_TAG),)
  DOCKER_IMG_TAG=latest
endif

TARGETS := dolphin-operator

.PHONY: all $(TARGETS) clean install

all: $(TARGETS)
dolphin-operator: GO_TAGS_FLAGS+=ipam_provider_operator

$(TARGETS):
  @$(ECHO_GO)
  $(QUIET) $(GO_BUILD) -o $(@)

$(TARGET)
  @$(ECHO_GO)
  $(QUIET) $(GO_BUILD) -o $@

clean:
  @$(ECHO_CLEAN)
  $(QUIET)rm -f $(TARGET)
  $(GO)clean $(GOCLEAN)

install:
  $(QUIET)$(INSTALL) -m 0755 -d $(DESTDIR)$(BINDIR)
  $(for each target, $(TARGETS), $(QUIET)$(INSTALL) -m 0755 $(target) $(DESTDIR)$(BINDIR);)
