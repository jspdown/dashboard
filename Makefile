.PHONY: help
help: ## List available targets
	@grep -hE '^[a-z0-9-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the dashboard
	nix build .#default

.PHONY: image
image: ## Build the OCI image
	nix build .#packages.x86_64-linux.image
	docker load < result

SYSTEM = $(shell nix eval --raw --impure --expr builtins.currentSystem)

.PHONY: lint
lint: ## Run all linters.
	nix build -L --no-link $(addprefix .#checks.$(SYSTEM)., lint-api lint-e2e lint-app lint-shell)

.PHONY: lint-go
lint-go: ## Run just the Go lint checks
	nix build -L --no-link $(addprefix .#checks.$(SYSTEM)., lint-api lint-e2e)

.PHONY: lint-app
lint-app: ## Run just the app lint checks
	nix build -L --no-link $(addprefix .#checks.$(SYSTEM)., lint-app)

.PHONY: check
check: ## Run all the checks
	nix run nixpkgs#nix-fast-build -- --no-nom --skip-cached --flake .#checks

.PHONY: test
test: test-unit test-e2e ## Run all tests

.PHONY: test-unit
test-unit: ## Run unit tests
	nix build -L --no-link .#checks.$(SYSTEM).test-unit

.PHONY: test-e2e
test-e2e: ## Run e2e tests
	nix run .#test-e2e

.PHONY: screenshots
screenshots: ## Regenerate canonical UI screenshots (OUT=dir overrides)
	nix run .#screenshots

.PHONY: dev-e2e
dev-e2e: ## Long-lived e2e stack + Vite HMR proxy
	nix run .#dev-e2e

.PHONY: fmt-nix
fmt-nix: ## Format and lint the Nix flake
	nix run .#fmt-nix

.PHONY: tidy
tidy: ## Tidy go.mod and regenerate the gomod2nix lockfile
	nix run .#tidy
