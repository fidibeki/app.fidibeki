FROM node:24-alpine AS build

ARG NUXT_PUBLIC_APP_URL

WORKDIR /src
RUN npm install --global pnpm@11.15.1

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY marketing/package.json marketing/package.json
RUN pnpm install --frozen-lockfile --filter @fidibeki/marketing...

COPY marketing marketing
RUN pnpm --filter @fidibeki/marketing build

FROM caddy:2-alpine

COPY marketing/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/marketing/.output/public /srv

EXPOSE 8080
