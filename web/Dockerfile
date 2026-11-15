# syntax=docker/dockerfile:1
# Needs the server, gamedata, errors, proto and edge build contexts, which Compose supplies: docker build --build-context server=../server --build-context gamedata=../server/data --build-context errors=../server/testdata/errors --build-context proto=../server/api/proto --build-context edge=../infra/nginx .
# The client build is the same on every platform, so it runs on the builder's own; only the nginx stage is per target.

# The maps as the client draws and walks them, built by the server's own code so their numbers match its (npm run maps).
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS maps
WORKDIR /src/server
COPY --from=server go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY --from=server . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go run ./cmd/mapjson -data data -out /out/maps

FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /src/web
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
# The build scripts read ../server/data, the error catalog and ../server/api/proto, as they do in the repository.
COPY --from=gamedata . /src/server/data
COPY --from=errors . /src/server/testdata/errors
COPY --from=proto . /src/server/api/proto
COPY . .
COPY --from=maps /out/maps public/maps
RUN npm run gen && npm run build --ignore-scripts
# Each file the browser downloads gets a copy gzipped at the highest level beside it, which nginx's gzip_static sends: the models too.
RUN find dist -type f \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.json' -o -name '*.svg' -o -name '*.glb' -o -name '*.gltf' -o -name '*.bin' \) -exec gzip -9 -k {} +

FROM nginx:1.30-alpine
COPY --from=edge finga.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/dist /usr/share/nginx/html
