# RInfra control plane (Go) — multi-stage build.
#
# Stage 1 builds a static binary; stage 2 ships it on a minimal base with CA
# certificates (needed for outbound TLS to cloud provider APIs).
#
# NOTE: live cloud provisioning (deploy/teardown) additionally requires the
# Pulumi CLI on PATH. It is intentionally NOT bundled here to keep the image
# lean — the API, engagement, audit, and emulation paths all run without it.
# Install Pulumi into a derived image if you need provisioning in-container.

# ---- build ----
FROM golang:1.24-alpine AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/rinfra-server ./cmd/rinfra-server

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 rinfra
COPY --from=build /out/rinfra-server /usr/local/bin/rinfra-server
USER rinfra
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/rinfra-server"]
