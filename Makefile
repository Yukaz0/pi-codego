.PHONY: build build-release build-go build-all install install-go uninstall test test-race vet clean run help

BINARY := bin/pi
BINARY_GO := bin/pi-go
PKG := ./...
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL_NAME ?= pi-go

help: ## Tampilkan bantuan
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## Build dev binary ke bin/pi (stamp version via -ldflags)
	@mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/pi
	@ls -lh $(BINARY)

build-release: ## Build optimized (~21MB) dengan -ldflags="-s -w"
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/pi
	@ls -lh $(BINARY)

build-go: ## Build versi Go sebagai pi-go (hindari bentrok pi npm)
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY_GO) ./cmd/pi
	@ls -lh $(BINARY_GO)

build-static: ## Build static untuk Docker scratch
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY)-linux-amd64 ./cmd/pi
	@ls -lh $(BINARY)-linux-amd64

build-all: ## Cross-compile semua platform dengan nama aset release (pi-go-{os}-{arch})
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-linux-amd64 ./cmd/pi
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-linux-arm64 ./cmd/pi
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-darwin-amd64 ./cmd/pi
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-darwin-arm64 ./cmd/pi
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-windows-amd64.exe ./cmd/pi
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/pi-go-windows-arm64.exe ./cmd/pi
	@ls -lh dist/

test: ## Verifikasi build (test files dihapus, gunakan vet)
	go vet ./... && go build ./...

test-verbose: ## Verifikasi verbose
	go vet -x ./...

test-race: ## Vet + build race (no test files)
	go vet ./... && echo "no *_test.go — gunakan codebase-memory-mcp untuk audit"

vet: ## go vet
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

fmt: ## Format code
	go fmt ./...

graph: ## Codebase memory check (via codebase-memory-mcp)
	@echo "Gunakan codebase-memory-mcp: list_projects -> search_graph -> trace_path"

run: build ## Build + jalankan TUI (butuh API key)
	./$(BINARY)

run-print: build ## Contoh print mode (butuh OPENAI_API_KEY)
	./$(BINARY) -p "tulis hello world di Go" --model openai/gpt-4o-mini

run-rpc: build ## Jalankan RPC mode
	./$(BINARY) --mode rpc

install: build-go ## Install global sebagai pi-go (tidak bentrok pi npm) ke $(BINDIR)/pi-go
	install -Dm755 $(BINARY_GO) $(DESTDIR)$(BINDIR)/$(INSTALL_NAME)
	@echo "✓ Terinstall: $(DESTDIR)$(BINDIR)/$(INSTALL_NAME)"
	@echo "  pi (npm)     : $$(which pi 2>/dev/null || echo 'tidak ditemukan')"
	@echo "  pi-go (Go)   : $(DESTDIR)$(BINDIR)/$(INSTALL_NAME)"
	@echo "  Tes: pi-go --help | pi-go -p \"halo\" --model ollama/llama3"

install-go: install ## Alias untuk install

uninstall: ## Hapus pi-go dari $(BINDIR)
	rm -f $(DESTDIR)$(BINDIR)/pi-go
	rm -f $(DESTDIR)$(BINDIR)/$(INSTALL_NAME)
	@echo "✓ pi-go dihapus dari $(DESTDIR)$(BINDIR)"

clean: ## Hapus binary & cache
	rm -rf bin/
	go clean -cache -testcache

check: vet test ## vet + build (CI)

size: build-release ## Tampilkan ukuran binary
	@ls -lh $(BINARY)
	@wc -c $(BINARY)

# Refresh the embedded model catalog from models.dev
catalog:
	go run scripts/generate_model_catalog.go -fetch
	gofmt -w pkg/provider/modelcatalog
