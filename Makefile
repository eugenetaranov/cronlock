.PHONY: build test test-cover test-integration test-all clean run lint fmt deps install release release-dry-run release-snapshot release-check

BINARY := bin/cronlock
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	@mkdir -p bin
	go build $(LDFLAGS) -o $(BINARY) ./cmd/cronlock

test:
	go test -v -race ./...

test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-integration:
	go test -v -tags=integration -timeout 5m ./integration/...

test-all: test test-integration

clean:
	rm -rf bin/ coverage.out coverage.html

run: build
	./$(BINARY) --config configs/cronlock.example.yaml

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...
	goimports -w .

deps:
	go mod download
	go mod tidy

install:
	go install $(LDFLAGS) ./cmd/cronlock

release:
	@echo "Creating a new release..."
	@read -p "Enter version (e.g., v0.1.0): " version; \
	git tag -a $$version -m "Release $$version"; \
	git push origin $$version

release-dry-run:
	goreleaser release --clean --skip=publish

release-snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check
