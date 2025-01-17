ARG BASE_IMAGE=scratch
ARG GOLANG_IMAGE=golang:1.23-alpine3.21

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