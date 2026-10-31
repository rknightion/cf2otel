# syntax=docker/dockerfile:1

FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
      -o /out/cf2otel ./cmd/cf2otel
# Checkpoint state directory, pre-owned by the distroless nonroot identity so a
# bind-mounted volume needs no separate chown step.
RUN install -d -m 0750 -o 65532 -g 65532 /out/state

# The static runtime supplies the CA bundle required for HTTPS to the Cloudflare
# API and OTLP, and has no shell or package manager. UID/GID 65532 is nonroot.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=build /out/cf2otel /usr/local/bin/cf2otel
COPY --from=build --chown=65532:65532 /out/state /var/lib/cf2otel
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/cf2otel", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/cf2otel"]
CMD []
