FROM golang:1.26.8-alpine AS build
ENV GOTOOLCHAIN=auto
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src
COPY . .
ARG VERSION=dev
# proxy.golang.org caches inconsistently for hanzoai/replicate@v0.6.0
# (different POPs serve different zip hashes). -mod=mod populates go.sum
# from whatever the proxy serves at build time and GOSUMDB=off skips
# sum.golang.org cross-checks.
RUN rm -f go.sum && CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" GOOS=linux \
    GOSUMDB=off go build -mod=mod \
    -ldflags "-s -w -linkmode external -extldflags '-static' -X main.version=${VERSION}" \
    -o /graphd ./cmd/graphd

# The binary, the TLS roots and the zoneinfo Go reads — nothing else.
# Statically linked, so no libc and no shell in the image at all.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /graphd /usr/local/bin/graphd
USER nonroot
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["graphd", "--version"]
ENTRYPOINT ["graphd"]
