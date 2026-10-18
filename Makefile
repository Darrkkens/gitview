# GitView, in the go-blueprint layout:
#   cmd/gitview   the CLI and terminal UI (main package)
#   internal/     analyses, exporters, internal/server (gitview serve), internal/webui
#   frontend/     the web UI: Vite + React + TypeScript + Tailwind CSS + daisyUI,
#                 built into frontend/dist/index.html (committed, embedded by Go)

BIN  ?= gitview
PORT ?= 7777
DIR  ?= .

all: build test

## build: the gitview binary (uses the committed web UI build)
build:
	@echo "Building..."
	@go build -o $(BIN) ./cmd/gitview

## install: go install ./cmd/gitview
install:
	@go install ./cmd/gitview

## web: rebuild the web UI into frontend/dist/index.html
web:
	@npm ci --prefix frontend --no-fund --no-audit
	@npm run build --prefix frontend

## run: open the web UI over DIR (gitview serve)
run: build
	@./$(BIN) serve $(DIR)

## dev: gitview serve on PORT plus Vite with hot reload; open the printed Vite URL
dev:
	@npm install --prefix frontend --no-fund --no-audit --prefer-offline
	@echo "UI with hot reload: http://localhost:5173/?t=dev"
	@trap 'kill 0' EXIT INT TERM; \
	GITVIEW_SERVE_TOKEN=dev go run ./cmd/gitview serve --open=false --port $(PORT) $(DIR) & \
	GITVIEW_API=http://127.0.0.1:$(PORT) npm run dev --prefix frontend

## watch: rebuild and restart gitview serve on Go changes (air, see .air.toml)
watch:
	@if command -v air > /dev/null; then \
		air; \
	else \
		echo "air is not installed: go install github.com/air-verse/air@latest"; exit 1; \
	fi

## test: Go tests and the web UI's type check
test:
	@go test ./...
	@if [ -d frontend/node_modules ]; then npm run typecheck --prefix frontend; fi

## web-check: the committed build matches the sources (CI)
web-check: web
	@git diff --exit-code --stat -- frontend/dist || \
		(echo "frontend/dist is stale: run 'make web' and commit it"; exit 1)

clean:
	@rm -f $(BIN)
	@rm -rf tmp

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

.PHONY: all build install web run dev watch test web-check clean help
