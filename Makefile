BINARY := cs
BUILD_DIR := bin

.PHONY: build clean test test-integration lint

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/cs

test:
	go test ./...

test-integration:
	./scripts/test-integration.sh

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.5 run ./...

clean:
	rm -rf $(BUILD_DIR)
