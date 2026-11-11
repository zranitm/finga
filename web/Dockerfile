# syntax=docker/dockerfile:1
# Needs the gamedata, errors, proto and edge build contexts, which Compose supplies: docker build --build-context gamedata=../server/data --build-context errors=../server/testdata/errors --build-context proto=../server/api/proto --build-context edge=../infra/nginx .
# The client build is the same on every platform, so it runs on the builder's own; only the nginx stage is per target.
FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /src/web
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
# The build scripts read ../server/data, the error catalog and ../server/api/proto, as they do in the repository.
COPY --from=gamedata . /src/server/data
COPY --from=errors . /src/server/testdata/errors
COPY --from=proto . /src/server/api/proto
COPY . .
RUN npm run build

FROM nginx:1.30-alpine
COPY --from=edge finga.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/dist /usr/share/nginx/html
