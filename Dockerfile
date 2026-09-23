# syntax=docker/dockerfile:1

FROM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/cf2otel /usr/local/bin/cf2otel
COPY --from=build --chown=65532:65532 /out/state /var/lib/cf2otel
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cf2otel"]
CMD []
