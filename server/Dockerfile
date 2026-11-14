# syntax=docker/dockerfile:1
# One Dockerfile, from server/, with a stage per image: finga (the game, the default), migrate and testers (docker build --target migrate .).
# Each image holds its binary and its config file at its server/ path, and runs from /app as from server/.
# The build stage runs on the builder's own platform and cross-compiles for the target (docker buildx build --platform linux/arm64), so an arm64 image needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
# The three binaries in one build, which every image's stage shares.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X finga/internal/host.version=${VERSION}" \
      -o /out/ ./cmd/finga ./cmd/migrate ./cmd/testers

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
USER nonroot
ENTRYPOINT ["/app/app"]

FROM runtime AS migrate
COPY --from=build /out/migrate /app/app
COPY --from=build /src/cmd/migrate/config/config.yaml /app/cmd/migrate/config/

FROM runtime AS testers
COPY --from=build /out/testers /app/app
COPY --from=build /src/cmd/testers/config/config.yaml /app/cmd/testers/config/
COPY data /app/data

FROM runtime AS finga
COPY --from=build /out/finga /app/app
COPY --from=build /src/config/config.yaml /app/config/
COPY data /app/data
