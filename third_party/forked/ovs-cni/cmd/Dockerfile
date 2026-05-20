FROM --platform=$BUILDPLATFORM quay.io/centos/centos:stream9 AS builder

RUN mkdir /workdir
# Support overriding target GOARCH during `make docker-build`
ARG goarch=

# these variable are automatically set during the multiarch build by docker buildx
ARG TARGETOS
ARG TARGETARCH
ENV TARGETOS=${TARGETOS:-linux}
ENV TARGETARCH=${TARGETARCH:-amd64}

ARG BUILDOS
ARG BUILDARCH
ENV BUILDOS=${BUILDOS:-linux}
ENV BUILDARCH=${BUILDARCH:-amd64}

ENV GOOS=${TARGETOS}
ENV GOARCH=${goarch:-$TARGETARCH}
ENV CGO_ENABLED=0
ENV GOFLAGS=-mod=vendor

WORKDIR /workdir

RUN dnf install -y wget

COPY go.mod .
COPY go.sum .

RUN GO_VERSION=$(sed -En 's/^toolchain +go(.*)$/\1/p' go.mod) && \
    wget https://dl.google.com/go/go${GO_VERSION}.${BUILDOS}-${BUILDARCH}.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.${BUILDOS}-${BUILDARCH}.tar.gz && \
    rm go${GO_VERSION}.${BUILDOS}-${BUILDARCH}.tar.gz

ENV PATH=/usr/local/go/bin:$PATH

RUN go mod download

COPY . .

RUN mkdir /workdir/bin
RUN go build -tags no_openssl -o /workdir/bin/ovs ./cmd/plugin

FROM registry.access.redhat.com/ubi9/ubi-minimal

# Explicitly upgrade libcap to fix CVE-2026-4878; --refresh ensures latest patched version is pulled
RUN microdnf --refresh install -y findutils libcap && microdnf clean all

COPY --from=builder /workdir/.version /.version
COPY --from=builder /workdir/bin/* /
