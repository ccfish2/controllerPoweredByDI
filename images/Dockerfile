ARG BASE_IMAGE=scratch
ARG GOLANG_IMAGE=docker.io/library/golang:1.21.9@sha256:7d0dcbe5807b1ad7272a598fbf9d7af15b5e2bed4fd6c4c2b5b3684df0b317dd
ARG ALPINE_IMAGE=docker.io/library/alpine:3.19.1@sha256:c5b1261d6d3e43071626931fc004f70149baeba2c8ec672bd4f27761f8e1ad6b

FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS builder

ARG TARGETOS
ARG TARGETARCH
ARG NOSTRIP
ARG NOOPT
ARG LOCKDEBUG
ARG RACE
ARG OPERATOR_VARIANT

WORKDIR /go/src/github.com/ccfish2/controllerPoweredByDI

RUN --mount=type=bind,readwrite,target=/go/src/github.com/ccfish2/controllerPoweredByDI \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg \
    make GOARCH=${TARGETARCH} RACE=${RACE} NOSTRIP=${NOSTRIP} NOOPT=${NOOPT} LOCKDEBUG=${LOCKDEBUG} \
    DESTDIR=/out/${TARGETOS}/${TARGETARCH} build-container-${OPERATOR_VARIANT} install-container-binary-${OPERATOR_VARIANT}

RUN --mount=type=bind,readwrite,target=/go/src/github.com/ccfish2/controllerPoweredByDI \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg \
    make GOARCH=${BUILDARCH} licenses-all && mv LICENSE.all /out/${TARGETOS}/${TARGETARCH}

FROM ${BASE_IMAGE} AS release
ARG TARGETOS
ARG TARGETARCH
ARG OPERATOR_VARIANT
COPY --from=builder /out/${TARGETOS}/${TARGETARCH}/usr/bin/dolphin-${OPERATOR_VARIANT} /usr/bin/dolphin-${OPERATOR_VARIANT}
WORKDIR /
CMD [ "/usr/bin/dolphin-${OPERATOR_VARIANT}" ]

FROM release AS debug 
ARG TARGETOS
ARG TARGETARCH
ARG OPERATOR_VARIANT
ARG DEBUG_HOLD
ENV DEBUG_PORT=2346
ENV DEBUG_HOLD=${DEBUG_HOLD}
COPY --from=builder /out/${TARGETOS}/${TARGETARCH}/usr/bin/dolphin-${OPERATOR_VARIANT} /usr/bin/dolphin-${OPERATOR_VARIANT}-bin