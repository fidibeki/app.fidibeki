FROM node:24-alpine AS widget-build

WORKDIR /src
RUN npm install --global pnpm@11.15.1

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY widget/package.json widget/package.json
RUN pnpm install --frozen-lockfile --filter @fidibeki/widget...

COPY contracts contracts
COPY widget widget
RUN pnpm --filter @fidibeki/widget build

FROM caddy:2.10.2-alpine

COPY infra/Caddyfile /etc/caddy/Caddyfile
COPY --from=widget-build /src/widget/dist /srv/widget
