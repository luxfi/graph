VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/graph ./cmd/graph

test:
	go test -v ./...

clean:
	rm -rf bin/

.PHONY: build test clean
