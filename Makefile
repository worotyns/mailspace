.DEFAULT_GOAL := help
.PHONY: help deps assets dev run seed build check test fmt docker clean dev-mail dev-sandbox
GOTOOLCHAIN ?= local
export GOTOOLCHAIN
help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
deps: ## Install pinned build-time dependencies (Node required only for assets)
	cd web && npm ci
assets: deps ## Bundle local HTMX/icons and compile Tailwind
	cd web && npm run build
dev: assets ## Run Go with templates/assets reloaded from disk; make assets after CSS edits
	go run ./cmd/devsandbox -dev
dev-sandbox: assets ## Run real app + synthetic TLS mail, users and organization on loopback
	go run ./cmd/devsandbox
dev-mail: ## Run isolated seeded IMAPS/SMTPS fixture (loopback only)
	go run ./cmd/devmail
run: ## Run the built binary (no Node required)
	./mailspace
seed: ## Validate and describe the bundled synthetic mailbox
	go run ./cmd/mailspace seed
build: assets ## Build a standalone binary and exact-commit source archive
	mkdir -p dist
	go run scripts/notices.go > dist/THIRD_PARTY_GO.txt
	git archive --format=tar.gz HEAD > dist/source.tar.gz
	go build -trimpath -o mailspace ./cmd/mailspace
check: assets ## Vet, race tests, formatting and workflows when actionlint is installed
	cd web && npm run format:check
	cd web && npm test
	go vet ./...
	go test -race ./...
	@if command -v actionlint >/dev/null 2>&1; then actionlint; else echo "actionlint unavailable: check separately"; fi
test: ## Run Go race tests (run make assets first after a fresh checkout)
	go test -race ./...
fmt: ## Format Go and asset source files
	gofmt -w cmd internal scripts
	cd web && npm run format

docker: ## Build and run the localhost Docker Compose demo
	docker compose up --build
clean: ## Remove local binary and generated source archive
	rm -f mailspace dist/source.tar.gz
