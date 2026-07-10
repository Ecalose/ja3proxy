.PHONY: all build-linux build-windows clean verify

GO?=go
GOVULNCHECK_VERSION?=v1.6.0
BINARY_NAME=ja3proxy
BINARY_DIR=bin
BINARY_LINUX=$(BINARY_DIR)/$(BINARY_NAME)
BINARY_WINDOWS=$(BINARY_DIR)/$(BINARY_NAME).exe
BUILD_FLAGS?=-trimpath
LDFLAGS?=-s -w

all: build-linux build-windows

$(BINARY_DIR):
	mkdir -p $(BINARY_DIR)

build-linux: $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_LINUX) ./cmd/ja3proxy

build-windows: $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_WINDOWS) ./cmd/ja3proxy

verify:
	$(GO) mod verify
	$(GO) mod tidy -diff
	$(GO) vet ./...
	$(GO) test -count=1 ./...
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

clean:
	rm -f $(BINARY_LINUX) $(BINARY_WINDOWS)
