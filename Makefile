BINARY  := sftpterm
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build run test lint fmt tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/sftpterm

run: build
	./bin/$(BINARY)

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

tidy:
	go mod tidy

clean:
	rm -rf bin dist
