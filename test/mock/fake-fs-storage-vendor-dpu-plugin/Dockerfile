# Fake fs-storage-vendor-dpu-plugin for SNAP e2e tests.
# Uses local directories instead of NFS mounts as backing storage.
# SNAP RPC calls are preserved so real device emulation still works.
#
# Needs its own Dockerfile (rather than the generic Dockerfile) because the
# Helm chart hardcodes the command as /fs-storage-vendor-dpu-plugin — the
# generic Dockerfile always outputs /manager.
# Follows the mock-dms pattern: binary, sources and Dockerfile all under
# test/mock/. internal/ is still copied because the fake reuses the real
# vendor plugin package.

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
      -o fs-storage-vendor-dpu-plugin \
      ./test/mock/fake-fs-storage-vendor-dpu-plugin

FROM ${base_image}
WORKDIR /
COPY --from=builder /workspace/fs-storage-vendor-dpu-plugin /fs-storage-vendor-dpu-plugin
