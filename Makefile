BINARY := cs
BUILD_DIR := bin
MAIN_PKG := ./cmd/cs
MACOS_SDK := $(shell xcrun --show-sdk-path 2>/dev/null)

# Default compilers for cross-compilation
ifeq ($(shell uname), Darwin)
    DARWIN_AMD64_CC ?= clang
    DARWIN_ARM64_CC ?= clang
    LINUX_AMD64_CC  ?= zig cc -target x86_64-linux
else
    DARWIN_AMD64_CC ?= zig cc -target x86_64-macos --sysroot=$(MACOS_SDK)
    DARWIN_ARM64_CC ?= zig cc -target aarch64-macos --sysroot=$(MACOS_SDK)
    LINUX_AMD64_CC  ?= zig cc -target x86_64-linux
endif

.PHONY: build clean test test-integration lint build-linux-amd64 build-darwin-amd64 build-darwin-arm64

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(BINARY) $(MAIN_PKG)

test:
	go test ./...

test-integration:
	./scripts/test-integration.sh

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.5 run ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY) $(BINARY)-linux-amd64 $(BINARY)-darwin-amd64 $(BINARY)-darwin-arm64

# Cross-compilation targets (requires zig for easy CGO cross-compilation when not on the native platform)
# You might need to install zig: brew install zig
build-linux-amd64:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="$(LINUX_AMD64_CC)" go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(MAIN_PKG)

build-darwin-amd64:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="$(DARWIN_AMD64_CC)" go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(MAIN_PKG)

build-darwin-arm64:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="$(DARWIN_ARM64_CC)" go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(MAIN_PKG)
