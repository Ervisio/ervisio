# LinuxAdmin — top-level build. Server targets build into server/bin/.
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/Fonlogen/LinuxAdmin/server/internal/brand.Version=$(VERSION)
DEV_LISTEN ?= 127.0.0.1:9090

.PHONY: build build-server test-server dev dev-noauth dev-dist build-web dev-web clean

build: build-web build-server

build-server:
	cd server && $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ ./cmd/... ./tools/...

test-server:
	cd server && $(GO) vet ./... && $(GO) test ./...

# Daemon in dev mode, proxying the UI to Vite (run `make dev-web` alongside).
dev: build-server
	./server/bin/linuxadmind --dev --listen $(DEV_LISTEN)

# Dev mode without sign-in (see server/README.md). Loopback only.
dev-noauth: build-server
	./server/bin/linuxadmind --dev --dev-insecure-noauth --listen $(DEV_LISTEN)

# Dev mode serving the built UI from web/dist.
dev-dist: build-server
	./server/bin/linuxadmind --dev --listen $(DEV_LISTEN) --web web/dist

build-web:
	npm --prefix web run build

dev-web:
	npm --prefix web run dev

clean:
	rm -rf server/bin
