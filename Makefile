GO ?= go
GOARCH ?= amd64
CMDS := $(notdir $(wildcard cmd/*))

.PHONY: help build-linux build-windows build-all test
.PHONY: $(CMDS:%=build-linux-%) $(CMDS:%=build-windows-%)

help:
	@echo "Available targets:"
	@echo "  build-linux    Build all binaries for Linux"
	@echo "  build-windows  Build all binaries for Windows (.exe)"
	@echo "  build-all      Build Linux and Windows binaries"
	@echo "  test           Run all tests"

build-linux: $(CMDS:%=build-linux-%)

build-linux-%:
	@mkdir -p bin/linux
	GOOS=linux GOARCH=$(GOARCH) $(GO) build -o bin/linux/$* ./cmd/$*

build-windows: $(CMDS:%=build-windows-%)

build-windows-%:
	@mkdir -p bin/windows
	GOOS=windows GOARCH=$(GOARCH) $(GO) build -o bin/windows/$*.exe ./cmd/$*

build-all: build-linux build-windows

test:
	$(GO) test ./...
