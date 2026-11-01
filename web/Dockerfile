# syntax=docker/dockerfile:1
# Needs the gamedata and edge contexts, which Compose supplies; on its own:
#   docker build --build-context gamedata=../server/data --build-context edge=../infra/nginx .
FROM node:24-alpine AS build
WORKDIR /src/web
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
# The build scripts read ../server/data, as they do in the repository.
COPY --from=gamedata . /src/server/data
COPY . .
RUN npm run build

FROM nginx:1.30-alpine
COPY --from=edge finga.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/web/dist /usr/share/nginx/html
