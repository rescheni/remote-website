.PHONY: build web all clean

BINARY_DIR=bin
GO=go
GOFLAGS=-ldflags="-s -w"

build: build-linux-amd64 build-linux-arm64 build-darwin-amd64

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayd-linux-amd64 ./cmd/relayd
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayc-linux-amd64 ./cmd/relayc

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayd-linux-arm64 ./cmd/relayd
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayc-linux-arm64 ./cmd/relayc

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayd-darwin-amd64 ./cmd/relayd
	GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY_DIR)/relayc-darwin-amd64 ./cmd/relayc

web:
	cd web && npm install && npm run build

all: web build

clean:
	rm -rf $(BINARY_DIR)
	rm -rf cmd/relayd/web/dist
	rm -rf web/node_modules
