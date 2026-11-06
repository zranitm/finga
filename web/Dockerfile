# syntax=docker/dockerfile:1
# Needs the gamedata, proto and edge build contexts, which Compose supplies: docker build --build-context gamedata=../server/data --build-context proto=../server/api/proto --build-context edge=../infra/nginx .
FROM node:24-alpine AS build
WORKDIR /src/web
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
# The build scripts read ../server/data and ../server/api/proto, as they do in the repository.
COPY --from=gamedata . /src/server/data
COPY --from=proto . /src/server/api/proto
COPY . .
RUN npm run build

FROM nginx:1.30-alpine
COPY --from=edge finga.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/dist /usr/share/nginx/html
