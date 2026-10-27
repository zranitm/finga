# syntax=docker/dockerfile:1
# One Dockerfile for every binary; the SERVICE build argument picks which:
# a service under services/ (item, ...) or a command under cmd/
# (gameserver, migrate). Build from server/ (infra/docker-compose.yml does):
#   docker build --build-arg SERVICE=item .
# Go's module and build caches are BuildKit cache mounts, so the images
# Compose builds side by side share one compilation.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG SERVICE=gameserver
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    pkg=./cmd/${SERVICE}; \
    if [ -d ./services/${SERVICE} ]; then pkg=./services/${SERVICE}/cmd/${SERVICE}; fi; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/app ${pkg}

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/app /app/app
COPY configs /app/configs
COPY data /app/data
USER nonroot
ENTRYPOINT ["/app/app"]
