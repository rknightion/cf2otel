# syntax=docker/dockerfile:1

FROM golang:1.27.3-bookworm@sha256:df4bb0305771186a85d0fd3a40f5c5da9af97ac8053d391ffce2c070817a20c6 AS build
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
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/cf2otel /usr/local/bin/cf2otel
COPY --from=build --chown=65532:65532 /out/state /var/lib/cf2otel
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/cf2otel", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/cf2otel"]
CMD []
