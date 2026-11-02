BINARY  := sftpterm
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

FUZZTIME ?= 30s
FUZZ_TARGETS := \
	./internal/sshx:FuzzParseDestination \
	./internal/units:FuzzParseRate \
	./internal/units:FuzzParseBytes \
	./internal/vfs:FuzzSanitize \
	./internal/vfs:FuzzCheckName \
	./internal/config:FuzzSaveRoundTrip

.PHONY: build run test lint fmt tidy clean cover fuzz golden

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/sftpterm

run: build
	./bin/$(BINARY)

test:
	go test -race ./...

cover:
	go test -coverpkg=./internal/... -coverprofile=cover.out ./...
	go tool cover -func=cover.out | tail -1

# Go fuzzes one target per run; failures are saved under testdata/fuzz.
fuzz:
	@set -e; for t in $(FUZZ_TARGETS); do \
		pkg=$${t%%:*}; name=$${t##*:}; \
		echo "fuzzing $$name ($(FUZZTIME))"; \
		go test -run '^$$' -fuzz "^$$name\$$" -fuzztime $(FUZZTIME) $$pkg; \
	done

# Rewrite UI snapshot files after an intentional change; review the diff.
golden:
	go test ./internal/ui -run Golden -update

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

tidy:
	go mod tidy

clean:
	rm -rf bin dist cover.out
