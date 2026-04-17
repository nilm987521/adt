MODULE     = github.com/adt-tool/adt
BINARY     = adt
LDFLAGS    = -ldflags "-s -w" -trimpath
VERSION    = $(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_FLAGS = -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" -trimpath

.PHONY: build build-all clean test

build:
	go build $(VERSION_FLAGS) -o dist/$(BINARY) ./cmd/adt

build-all:
	GOOS=darwin  GOARCH=arm64 go build $(VERSION_FLAGS) -o dist/$(BINARY)-darwin-arm64  ./cmd/adt
	GOOS=darwin  GOARCH=amd64 go build $(VERSION_FLAGS) -o dist/$(BINARY)-darwin-amd64  ./cmd/adt
	GOOS=windows GOARCH=amd64 go build $(VERSION_FLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/adt
	GOOS=linux   GOARCH=amd64 go build $(VERSION_FLAGS) -o dist/$(BINARY)-linux-amd64   ./cmd/adt
	GOOS=linux   GOARCH=arm64 go build $(VERSION_FLAGS) -o dist/$(BINARY)-linux-arm64   ./cmd/adt

clean:
	rm -rf dist/

test:
	go test ./...
