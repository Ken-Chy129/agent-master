BINARY  := agent-master
PKG     := github.com/Ken-Chy129/agent-master
VERSION ?= 0.0.1-dev
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
WEB_DIST := frontend/apps/web/dist
EMBED_WEB_DIST := internal/webui/dist

.PHONY: build run tidy test vet clean release frontend-deps web-assets

## frontend-deps: install pinned Web dependencies when missing or stale
frontend-deps:
	@if [ ! -f frontend/node_modules/.package-lock.json ] || [ frontend/package-lock.json -nt frontend/node_modules/.package-lock.json ]; then \
		npm ci --prefix frontend; \
	fi

## web-assets: build the browser client and stage it for Go embedding
web-assets: frontend-deps
	npm run build -w @agent-master/web --prefix frontend
	@find $(EMBED_WEB_DIST) -type f ! -name .keep -delete
	@cp -R $(WEB_DIST)/. $(EMBED_WEB_DIST)/

## build: build the daemon for the current platform (static, no cgo)
build: web-assets
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
	@find $(EMBED_WEB_DIST) -type f ! -name .keep -delete

## release: cross-compile static binaries + one checksum manifest
release: web-assets
	@mkdir -p dist
	@rm -f dist/agent-master-* dist/SHA256SUMS
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		if [ "$$os" = windows ]; then ext=".exe"; fi; \
		name=$(BINARY)-$$os-$$arch$$ext; out=dist/$$name; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$out ./cmd/agent-master || exit 1; \
		( cd dist && sha256sum $$name >> SHA256SUMS ); \
	done
	@echo "release artifacts in dist/"
