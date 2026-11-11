# Build from repository src: docker build -f go-service/Dockerfile .
# Prefer deploy/Dockerfile: its adjacent ignore file filters the src context.
ARG GO_VERSION=1.27
FROM node:22-bookworm-slim AS frontend
WORKDIR /src/ui
COPY ui/package.json ./
# This repository has a pnpm lock, not an npm lock. npm resolves package ranges.
RUN npm install --no-audit --no-fund
COPY ui/ ./
RUN npm run build && npm run build:admin

FROM golang:${GO_VERSION}-bookworm AS backend
WORKDIR /src/go-service
COPY go-service/go.mod go-service/go.sum ./
RUN go mod download
COPY go-service/ ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/comic-server ./cmd/server

FROM debian:bookworm-slim AS runtime
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates wget tzdata && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 comic && useradd --uid 10001 --gid comic --no-create-home comic \
    && mkdir -p /app/data /app/public && chown -R comic:comic /app
WORKDIR /app
COPY --from=backend /out/comic-server /app/comic-server
COPY --from=frontend /src/web/public/ui /app/public/ui
COPY --from=frontend /src/web/public/admin /app/public/admin
ENV GO_SERVICE_ADDR=0.0.0.0:5210 GO_SERVICE_DATA_ROOT=/app/data GO_SERVICE_PUBLIC_DIR=/app/public
USER 10001:10001
EXPOSE 5210
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:5210/api/health || exit 1
ENTRYPOINT ["/app/comic-server"]
