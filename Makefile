GO ?= go
GOARCH ?= amd64
CMDS := $(patsubst cmd/%/main.go,%,$(wildcard cmd/*/main.go))

.PHONY: help build-linux build-windows build-all test

help:
	@echo "Available targets:"
	@echo "  build-linux    Build all binaries for Linux"
	@echo "  build-windows  Build all binaries for Windows (.exe)"
	@echo "  build-all      Build Linux and Windows binaries"
	@echo "  test           Run all tests"

build-linux:
	@test -n "$(CMDS)" || (echo "No commands found under cmd/*/main.go" && exit 1)
	@mkdir -p bin/linux
	@for cmd in $(CMDS); do \
		echo "Building $$cmd for linux (static, CGO_ENABLED=0)"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) build -o bin/linux/$$cmd ./cmd/$$cmd || exit $$?; \
	done

build-windows:
	@test -n "$(CMDS)" || (echo "No commands found under cmd/*/main.go" && exit 1)
	@mkdir -p bin/windows
	@for cmd in $(CMDS); do \
		echo "Building $$cmd for windows"; \
		GOOS=windows GOARCH=$(GOARCH) $(GO) build -o bin/windows/$$cmd.exe ./cmd/$$cmd || exit $$?; \
	done

build-all: build-linux build-windows

test:
	$(GO) test ./...
