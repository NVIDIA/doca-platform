ARG builder_image
ARG base_image

# Build the mock-dpuagent and mock-reboot-controller binaries (no emulation)
FROM --platform=${BUILDPLATFORM} ${builder_image} AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the go source (specific directories to improve cache efficiency)
COPY api/ api/
COPY cmd/ cmd/
COPY test/ test/
COPY internal/ internal/
COPY pkg/ pkg/

ARG gcflags
ARG ldflags
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="${ldflags}"  \
    -gcflags="${gcflags}" \
    -o mock-dpuagent ./test/mock/dpuagent

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="${ldflags}"  \
    -gcflags="${gcflags}" \
    -o mock-reboot-controller ./test/mock/dpuagent/rebootcontroller

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="${ldflags}"  \
    -gcflags="${gcflags}" \
    -o md5sum ./test/mock/md5sum

FROM ${base_image}
WORKDIR /
COPY --from=builder /workspace/mock-dpuagent .
COPY --from=builder /workspace/mock-reboot-controller .
COPY --from=builder /workspace/md5sum /bin/

# The mock binds the privileged BMC ports 443 and 80, so it runs as root with only
# CAP_NET_BIND_SERVICE kept (see the chart's securityContext).
USER 0:0

ENTRYPOINT ["/mock-dpuagent"]
