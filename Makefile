.PHONY: build test install clean ui

BINARY_NAME = workflow-plugin-infra
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-X github.com/GoCodeAlone/workflow-plugin-infra/internal.Version=$(VERSION)"
INSTALL_DIR ?= data/plugins/$(BINARY_NAME)
INSTALL_PATH = $(if $(DESTDIR),$(DESTDIR)/$(INSTALL_DIR),$(INSTALL_DIR))
GO_ENV = GOWORK=off GOPRIVATE=github.com/GoCodeAlone/*

build: ui
	$(GO_ENV) go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

ui:
	cd ui && npm ci && npx vite build
	rm -rf internal/ui_dist && cp -r ui/dist internal/ui_dist

test:
	$(GO_ENV) go test ./... -v -race

install: build
	mkdir -p $(INSTALL_PATH)
	cp bin/$(BINARY_NAME) $(INSTALL_PATH)/
	cp plugin.json $(INSTALL_PATH)/
	cp plugin.contracts.json $(INSTALL_PATH)/

clean:
	rm -rf bin/ internal/ui_dist/
