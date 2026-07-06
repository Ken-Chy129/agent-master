BINARY  := agent-master
PKG     := github.com/Ken-Chy129/agent-master
VERSION ?= 0.0.1-dev
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build run tidy test vet clean release

## build: build the daemon for the current platform (static, no cgo)
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/agent-master

## run: build then serve
run: build
	./dist/$(BINARY) serve

tidy:
	go mod tidy

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf dist

## release: cross-compile static binaries + sha256 for all platforms
release:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; out=dist/$(BINARY)-$$os-$$arch; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$out ./cmd/agent-master || exit 1; \
		( cd dist && sha256sum $(BINARY)-$$os-$$arch > $(BINARY)-$$os-$$arch.sha256 ); \
	done
	@echo "release artifacts in dist/"
