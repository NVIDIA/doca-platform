# syntax=docker/dockerfile:1.4

# Build image to compile all packages
# The target repository is  https://github.com/NVIDIA/doca-sosreport
FROM ubuntu:24.04 AS build

ARG PYTHON_VERSION=3.12

RUN apt-get update -qy && apt-get install -qyy \
    -o APT::Install-Recommends=false \
    -o APT::Install-Suggests=false \
    build-essential \
    ca-certificates \
    curl \
    python3-setuptools \
    python${PYTHON_VERSION}-dev && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:0.4.10 /uv /usr/local/bin/uv

ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_PYTHON_DOWNLOADS=never \
    UV_PYTHON=python3.12

ENV VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH"

COPY setup.py .
COPY doca-dpf.py sos/report/plugins/doca-dpf.py
COPY sos sos
COPY bin bin
COPY man man
COPY tmpfiles tmpfiles
COPY LICENSE .
COPY AUTHORS .
COPY README.md .
COPY sos.conf .
COPY sos-mlx-cloud-verification.conf .

RUN --mount=type=cache,target=/root/.cache \
    uv venv /opt/venv && uv pip install .

FROM ubuntu:24.04

ARG TARGETARCH
ARG KUBERNETES_VERSION=1.30
ARG MFT_VERSION=4.29.0-131
ARG PYTHON_VERSION=3.12

ENV TAR_OPTIONS="--no-same-owner"

RUN apt-get update && apt-get install -y apt-transport-https ca-certificates gpg curl pciutils

RUN apt-get update -qy && apt-get install -qyy \
    -o APT::Install-Recommends=false \
    -o APT::Install-Suggests=false \
    python${PYTHON_VERSION} \
    libpython${PYTHON_VERSION} && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/root/.cache \
    case ${TARGETARCH} in \
        amd64) curl -fsSL https://www.mellanox.com/downloads/MFT/mft-${MFT_VERSION}-x86_64-deb.tgz | tar -xz -C /tmp && \
                cd /tmp/mft-${MFT_VERSION}-x86_64-deb && \
                ./install.sh --without-kernel ;; \
        arm64) curl -fsSL https://www.mellanox.com/downloads/MFT/mft-${MFT_VERSION}-arm64-deb.tgz | tar -xz -C /tmp && \
                cd /tmp/mft-${MFT_VERSION}-arm64-deb && \
                ./install.sh --without-kernel ;; \
        *) echo "Unsupported architecture: ${TARGETARCH}" && exit 1 ;; \
    esac

# TODO: If we plan support for additional container runtimes, we need to install the corresponding packages here.
RUN --mount=type=cache,target=/root/.cache \
    curl -fsSL https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_VERSION}/deb/Release.key \
    | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg && \
    echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_VERSION}/deb/ /" \
    | tee /etc/apt/sources.list.d/kubernetes.list && \
    apt-get update && apt-get install -y cri-tools kubectl

COPY --from=build /opt/venv /opt/venv
COPY sos-mlx-cloud-verification.conf /etc/sos/sos.conf

COPY --chmod=0755 dpf-report.sh /usr/local/bin/dpf-report

ENV PATH="/opt/venv/bin:$PATH"
