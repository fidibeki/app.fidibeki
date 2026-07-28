FROM node:24-alpine AS build

WORKDIR /src
RUN npm install --global pnpm@11.15.1

COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY backoffice/package.json backoffice/package.json
RUN pnpm install --frozen-lockfile --filter @fidibeki/backoffice...

COPY web/assets/css/main.css web/assets/css/main.css
COPY backoffice backoffice
RUN pnpm --filter @fidibeki/backoffice build

FROM node:24-alpine

ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000

WORKDIR /app
COPY --from=build --chown=node:node /src/backoffice/.output ./

USER node
EXPOSE 3000
CMD ["node", "server/index.mjs"]
