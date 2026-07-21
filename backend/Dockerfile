FROM golang:1.25.12-alpine AS build

WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download

COPY backend ./backend
RUN cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fidibeki-api ./cmd/fidibeki-api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S fidibeki \
    && adduser -S -G fidibeki fidibeki \
    && mkdir -p /var/lib/fidibeki-emails \
    && chown fidibeki:fidibeki /var/lib/fidibeki-emails

WORKDIR /app
COPY --from=build /out/fidibeki-api /usr/local/bin/fidibeki-api
COPY infra/migrations /app/migrations

USER fidibeki
EXPOSE 8080
ENTRYPOINT ["fidibeki-api"]
CMD ["serve"]
