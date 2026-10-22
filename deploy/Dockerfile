# One Dockerfile for both binaries; CMD picks which (gameserver, migrate).
# Build from the repository root:
#   docker build -f deploy/Dockerfile --build-arg CMD=gameserver .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD=gameserver
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/app /app/app
COPY configs /app/configs
COPY data /app/data
USER nonroot
ENTRYPOINT ["/app/app"]
