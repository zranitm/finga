# syntax=docker/dockerfile:1
# One Dockerfile for every binary; the SERVICE build argument picks which:
# a service under services/ (gateway, item, ...) or a command under cmd/
# (migrate). Build from server/ (infra/docker-compose.yml does):
#   docker build --build-arg SERVICE=item .
# The image holds that binary's own configuration file and nothing of the
# others', at the path it has under server/ (services/item/config/config.yaml),
# since the binary runs from /app as it runs from server/.
# Go's module and build caches are BuildKit cache mounts, so the images
# Compose builds side by side share one compilation.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG SERVICE=gateway
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    pkg=./cmd/${SERVICE}; cfg=cmd/${SERVICE}/config; \
    if [ -d ./services/${SERVICE} ]; then pkg=./services/${SERVICE}/cmd/${SERVICE}; cfg=services/${SERVICE}/config; fi; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/app ${pkg} && \
    mkdir -p /out/root/${cfg} && cp ${cfg}/config.yaml /out/root/${cfg}/

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /out/root/ /app/
COPY data /app/data
USER nonroot
ENTRYPOINT ["/app/app"]
