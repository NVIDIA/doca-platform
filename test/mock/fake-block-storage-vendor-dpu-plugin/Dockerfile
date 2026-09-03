# Fake block-storage-vendor-dpu-plugin for SNAP e2e tests.
# It uses sparse local files and SPDK AIO bdevs instead of an external NVMe-oF target.

ARG builder_image
ARG base_image

FROM --platform=${BUILDPLATFORM} ${builder_image} AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY api/ api/
COPY internal/ internal/
COPY test/ test/

ARG gcflags
ARG ldflags

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false \
      -ldflags="${ldflags}" -gcflags="${gcflags}" \
      -o block-storage-vendor-dpu-plugin \
      ./test/mock/fake-block-storage-vendor-dpu-plugin

FROM ${base_image}
WORKDIR /
COPY --from=builder /workspace/block-storage-vendor-dpu-plugin /block-storage-vendor-dpu-plugin
