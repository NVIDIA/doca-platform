FROM --platform=${BUILDPLATFORM} golang:1.23 as builder

RUN mkdir /workdir
WORKDIR /workdir

COPY . .

ENV GOOS linux
# Support overriding target GOARCH during `make docker-build`
ARG goarch=amd64
ENV GOARCH=$goarch
ENV CGO_ENABLED 0

RUN mkdir /workdir/bin
RUN go build -tags no_openssl -o /workdir/bin/ovs ./cmd/plugin
RUN go build -tags no_openssl -o /workdir/bin/marker ./cmd/marker
RUN go build -tags no_openssl -o /workdir/bin/ovs-mirror-producer ./cmd/mirror-producer
RUN go build -tags no_openssl -o /workdir/bin/ovs-mirror-consumer ./cmd/mirror-consumer

# Copy the go source code so it can be distributed in the final image.
RUN go mod vendor
RUN mkdir src && \
    find . -name '*.go' -exec cp --parents \{\}  src/ \; && \
    tar -czf source-code.tar.gz src

FROM nvcr.io/nvidia/base/ubuntu:22.04_20240212

COPY --from=builder /workdir/source-code.tar.gz source-code.tar.gz

COPY --from=builder /workdir/.version /.version
COPY --from=builder /workdir/bin/* /
