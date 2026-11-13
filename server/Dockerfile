# syntax=docker/dockerfile:1
# One Dockerfile for every binary, from server/ (docker build --build-arg SERVICE=item .); the image holds only its config file, at its server/ path, and runs from /app as from server/.
# The build stage runs on the builder's own platform and cross-compiles for the target (docker buildx build --platform linux/arm64), so an arm64 image needs no emulation; a plain docker build is unchanged.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH
# Every binary's packages, in a step the same for every image: one build of several images (make up's) compiles them once, and each image then only links.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    export CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} && \
    mains=$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...) && \
    go build -trimpath ${mains}
ARG SERVICE=gateway
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    pkg=./cmd/${SERVICE}; cfg=cmd/${SERVICE}/config; \
    if [ -d ./services/${SERVICE} ]; then cfg=config; fi; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X finga/internal/host.version=${VERSION}" -o /out/app ${pkg} && \
    mkdir -p /out/root/${cfg} && cp ${cfg}/config.yaml /out/root/${cfg}/

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/app /app/app
COPY --from=build /out/root/ /app/
COPY data /app/data
USER nonroot
ENTRYPOINT ["/app/app"]
