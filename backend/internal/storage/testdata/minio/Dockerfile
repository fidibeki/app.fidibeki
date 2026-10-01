# The upstream container tag was removed. Build the same release from its
# immutable source commit so storage integration tests keep a real S3 server.
FROM golang:1.25.13-alpine AS build
RUN apk add --no-cache git
RUN git clone --depth 1 --branch RELEASE.2025-09-07T16-13-09Z https://github.com/minio/minio.git /src \
    && test "$(git -C /src rev-parse HEAD)" = "07c3a429bfed433e49018cb0f78a52145d4bedeb"
WORKDIR /src
RUN CGO_ENABLED=0 go build -trimpath -o /out/minio .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/minio /usr/local/bin/minio
ENTRYPOINT ["minio"]
