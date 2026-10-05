# Ervisio — top-level build. Server targets build into server/bin/.
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/ervisio/ervisio/server/internal/brand.Version=$(VERSION)
DEV_LISTEN ?= 127.0.0.1:9090

.PHONY: build build-server build-windows test-server dev dev-noauth dev-dist build-web dev-web dist clean

build: build-web build-server

build-server:
	cd server && $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ ./cmd/... ./tools/...

# Windows binaries (pure Go, no cgo): server/bin/windows-<arch>/{ervisiod,ervisio-bridge}.exe
WIN_ARCHS ?= amd64 arm64
build-windows:
	@for arch in $(WIN_ARCHS); do \
		echo "windows/$$arch"; \
		(cd server && CGO_ENABLED=0 GOOS=windows GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o bin/windows-$$arch/ervisiod.exe ./cmd/ervisiod && \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o bin/windows-$$arch/ervisio-bridge.exe ./cmd/ervisio-bridge) || exit 1; \
	done

test-server:
	cd server && $(GO) vet ./... && $(GO) test ./...

# Daemon in dev mode, proxying the UI to Vite (run `make dev-web` alongside).
dev: build-server
	./server/bin/ervisiod --dev --listen $(DEV_LISTEN)

# Dev mode without sign-in (see server/README.md). Loopback only.
dev-noauth: build-server
	./server/bin/ervisiod --dev --dev-insecure-noauth --listen $(DEV_LISTEN)

# Dev mode serving the built UI from web/dist.
dev-dist: build-server
	./server/bin/ervisiod --dev --listen $(DEV_LISTEN) --web web/dist

build-web:
	npm --prefix web run build

dev-web:
	npm --prefix web run dev

# Release archive for this machine's architecture, as the release workflow
# builds it: make dist VERSION=1.2.3  ->  dist/ervisio-1.2.3-linux-<arch>.tar.gz
# (and the compatibility archive dist/linuxadmin-1.2.3-linux-<arch>.tar.gz)
dist: build
	./packaging/build-release.sh $(VERSION) $(shell cd server && $(GO) env GOARCH) dist

clean:
	rm -rf server/bin dist
