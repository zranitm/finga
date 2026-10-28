# syntax=docker/dockerfile:1
# The client as make up serves it: the production build on nginx, with /api
# and /ws passed to the gateway (infra/nginx/finga.conf). Besides web/, the
# build reads two named contexts, which infra/docker-compose.yml supplies:
# gamedata, the server's game data the client generates its tables and maps
# from, and edge, the nginx configuration. On its own:
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
