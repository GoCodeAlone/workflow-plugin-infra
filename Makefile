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

test: _ensure_ui_dist
	$(GO_ENV) go test ./... -v -race

# _ensure_ui_dist: build the SPA only when internal/ui_dist/ is absent.
# This makes `make test` self-contained after `make clean` without forcing
# a full npm/vite rebuild on every run when internal/ui_dist/ already exists
# (e.g. in CI where it is checked in).
_ensure_ui_dist:
	@if [ ! -f internal/ui_dist/index.html ]; then \
		echo "internal/ui_dist missing — building SPA..."; \
		$(MAKE) ui; \
	fi

install: build
	mkdir -p $(INSTALL_PATH)
	cp bin/$(BINARY_NAME) $(INSTALL_PATH)/
	cp plugin.json $(INSTALL_PATH)/
	cp plugin.contracts.json $(INSTALL_PATH)/

clean:
	rm -rf bin/ internal/ui_dist/
