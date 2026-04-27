.PHONY: build test install clean

BINARY_NAME = workflow-plugin-infra
INSTALL_DIR ?= data/plugins/$(BINARY_NAME)
INSTALL_PATH = $(if $(DESTDIR),$(DESTDIR)/$(INSTALL_DIR),$(INSTALL_DIR))
GO_ENV = GOWORK=off GOPRIVATE=github.com/GoCodeAlone/*

build:
	$(GO_ENV) go build -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

test:
	$(GO_ENV) go test ./... -v -race

install: build
	mkdir -p $(INSTALL_PATH)
	cp bin/$(BINARY_NAME) $(INSTALL_PATH)/
	cp plugin.json $(INSTALL_PATH)/
	cp plugin.contracts.json $(INSTALL_PATH)/

clean:
	rm -rf bin/
