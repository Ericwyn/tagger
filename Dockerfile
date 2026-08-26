# syntax=docker/dockerfile:1

ARG NODE_VERSION=24
ARG GO_VERSION=1.25.7
ARG ALPINE_VERSION=3.23

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS frontend
WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
COPY --from=frontend /src/frontend/dist/ ./web/dist/

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath \
      -ldflags "-s -w -X github.com/ericwyn/tagger/internal/version.Version=${VERSION}" \
      -o /out/tagger ./cmd/tagger

FROM alpine:${ALPINE_VERSION} AS runtime

RUN mkdir -p /data /music \
    && chown -R 10001:10001 /data /music

COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend /usr/local/go/lib/time/zoneinfo.zip /usr/share/zoneinfo.zip
COPY --from=backend --chown=10001:10001 /out/tagger /usr/local/bin/tagger

ENV TAGGER_LISTEN=0.0.0.0:8080 \
    TAGGER_MUSIC_DIR=/music \
    TAGGER_DATA_DIR=/data \
    ZONEINFO=/usr/share/zoneinfo.zip

WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080

USER 10001:10001
ENTRYPOINT ["/usr/local/bin/tagger"]
