ARG BASE_IMAGE=scratch
ARG GOLANG_IMAGE=golang:1.23.5

# Stage 1: Build the Go binary
FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS builder

ARG TARGETOS
ARG TARGETARCH
ARG GO_TAGS_FLAGS="home_provider_operator"

WORKDIR /app

# Cache modules
COPY ../go.mod ../go.sum ./
RUN go mod download

COPY ../ .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags "${GO_TAGS_FLAGS}" -o dolphin-operator-generic .

# Stage 2: Minimal runtime image
FROM scratch AS release

COPY --from=builder /app/dolphin-operator-generic /usr/bin/dolphin-operator

ENTRYPOINT ["/usr/bin/dolphin-operator"]
CMD ["--enable-k8s", "--k8s-kubeconfig-path=/etc/kubernetes/admin.conf"]