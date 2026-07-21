FROM node:24-alpine AS build

WORKDIR /src
RUN npm install --global pnpm@11.15.1

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY web/package.json web/package.json
RUN pnpm install --frozen-lockfile --filter @fidibeki/web...

COPY web web
RUN pnpm --filter @fidibeki/web build

FROM node:24-alpine

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

WORKDIR /app
COPY --from=build --chown=node:node /src/web/.output ./

USER node
EXPOSE 3000
CMD ["node", "server/index.mjs"]
