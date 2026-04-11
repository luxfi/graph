# Stage 1: Build
FROM golang:1.26-alpine AS build
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /graph ./cmd/graph

# Stage 2: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S graph && adduser -S graph -G graph
COPY --from=build /graph /usr/local/bin/graph
USER graph
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["graph", "--version"]
ENTRYPOINT ["graph"]
