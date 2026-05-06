FROM golang:1.26-alpine AS build
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY go.mod go.sum ./
# proxy.golang.org has inconsistent caching for hanzoai/replicate@v0.6.0
# (different POPs serve different zip hashes). Regenerate go.sum from the
# proxy state we actually see at build time and skip sum.golang.org.
RUN rm -f go.sum && GOSUMDB=off go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /graphd ./cmd/graphd

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S graphd && adduser -S graphd -G graphd
COPY --from=build /graphd /usr/local/bin/graphd
USER graphd
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["graphd", "--version"]
ENTRYPOINT ["graphd"]
