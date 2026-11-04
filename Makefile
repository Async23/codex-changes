VERSION ?= dev
INSTALL_DIR ?= $(HOME)/.local/bin
BUILD_FLAGS = -trimpath -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build check test demo install

build:
	go build $(BUILD_FLAGS) -o dist/codex-changes ./cmd/codex-changes

check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go mod tidy -diff
	go vet ./...
	go test -race ./...

test:
	go test ./...

demo: build
	./dist/codex-changes --demo

install:
	mkdir -p "$(INSTALL_DIR)"
	go build $(BUILD_FLAGS) -o "$(INSTALL_DIR)/codex-changes" ./cmd/codex-changes
