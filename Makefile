# pipit Makefile
#
# This Makefile provides a unified interface for common development tasks.
# Every recipe delegates to a script under hack/; the rationale for what each
# one does lives in that script's header, not here.
#
# Run `make help` to see available targets.

.DEFAULT_GOAL := help

# Directories
HACK_DIR := ./hack
BIN_DIR := ./bin

# Go settings
GO ?= go

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-27s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build-cli
build-cli: ## Build the CLI binary for the current platform
	@$(HACK_DIR)/build/build.sh current $(BIN_DIR)/pipit

.PHONY: build-cli-all
build-cli-all: ## Build CLI binaries for every release platform
	@$(HACK_DIR)/build/build.sh all $(BIN_DIR)/cli

.PHONY: install-cli
install-cli: ## Build and install the CLI binary into GOBIN
	@$(HACK_DIR)/build/build.sh install

.PHONY: build-wasm
build-wasm: ## Build, optimise, and compress the WASM playground binary
	@$(HACK_DIR)/wasm/build.sh build

.PHONY: build-wasm-clean
build-wasm-clean: ## Clean WASM build artefacts
	@$(HACK_DIR)/wasm/build.sh clean

##@ Test

.PHONY: test
test: ## Run both modules in the default lane
	@$(HACK_DIR)/test/test.sh quick

.PHONY: test-short
test-short: ## Run both modules with -short
	@$(HACK_DIR)/test/test.sh short

.PHONY: test-safe
test-safe: ## Run the interpreter in the pure-Go lane
	@$(HACK_DIR)/test/test.sh safe

.PHONY: test-race
test-race: ## Run the interpreter under the race detector
	@$(HACK_DIR)/test/test.sh race

.PHONY: test-torture
test-torture: ## Run both GC-torture lanes
	@$(HACK_DIR)/test/test.sh torture

.PHONY: test-torture-default
test-torture-default: ## GC-torture lane over the default build
	@$(HACK_DIR)/test/test.sh torture-default

.PHONY: test-torture-safe
test-torture-safe: ## GC-torture lane over the pure-Go build
	@$(HACK_DIR)/test/test.sh torture-safe

.PHONY: test-torture-paranoid
test-torture-paranoid: ## GC torture plus register-arena poisoning
	@$(HACK_DIR)/test/test.sh torture-paranoid

.PHONY: test-golden
test-golden: ## Compare the golden disassembly snapshots
	@$(HACK_DIR)/test/test.sh golden

.PHONY: test-golden-update
test-golden-update: ## Re-record the golden disassembly snapshots
	@$(HACK_DIR)/test/test.sh golden-update

.PHONY: test-integration
test-integration: ## Run every integration suite (the parity corpus, language, bytecode, ...)
	@$(HACK_DIR)/test/test.sh integration

.PHONY: test-isolation
test-isolation: ## Run the Linux sandbox tests with no skip fallback
	@$(HACK_DIR)/test/test.sh isolation

.PHONY: test-gotoolchain
test-gotoolchain: ## Run the Go toolchain corpus through the CLI binary
	@$(HACK_DIR)/test/test.sh gotoolchain

.PHONY: test-gotoolchain-update
test-gotoolchain-update: ## Re-record the passing Go toolchain set
	@$(HACK_DIR)/test/test.sh gotoolchain-update

.PHONY: test-all
test-all: ## Run the default, pure-Go and golden lanes
	@$(HACK_DIR)/test/test.sh all

.PHONY: test-coverage-total
test-coverage-total: ## Report library statement coverage
	@$(HACK_DIR)/test/coverage.sh total

.PHONY: test-coverage-cli-total
test-coverage-cli-total: ## Report CLI statement coverage
	@$(HACK_DIR)/test/coverage.sh cli-total

.PHONY: test-update-badges
test-update-badges: ## Re-measure coverage and rewrite the README shields
	@$(HACK_DIR)/test/coverage.sh update-badges

##@ Benchmark

.PHONY: bench
bench: ## Run the interpreter benchmarks. ARGS='--filter Dispatch --count 10' narrows the sweep
	@$(HACK_DIR)/bench/bench.sh run $(ARGS)

.PHONY: bench-stability
bench-stability: ## Run at -count=10 and print the run-to-run spread
	@$(HACK_DIR)/bench/bench.sh stability $(ARGS)

##@ Lint

.PHONY: lint
lint: lint-go lint-scripts ## Run all linters

.PHONY: lint-go
lint-go: ## Lint every Go module with golangci-lint (includes the layering rules)
	@$(HACK_DIR)/lint/go.sh $(ARGS)

.PHONY: lint-scripts
lint-scripts: ## Lint every hack/ script with shellcheck
	@$(HACK_DIR)/lint/scripts.sh

.PHONY: lint-vet
lint-vet: ## Run go vet over the default build configuration
	@$(HACK_DIR)/lint/vet.sh default

.PHONY: lint-vet-tags
lint-vet-tags: ## Vet every build tag and cross-compile target
	@$(HACK_DIR)/lint/vet.sh tags

##@ Go

.PHONY: go-mod-tidy
go-mod-tidy: ## Tidy the go.mod files that can be tidied
	@$(HACK_DIR)/go/mod-tidy.sh

.PHONY: go-mod-update
go-mod-update: ## Update every module's direct dependencies. ARGS=cmd/pipit narrows it to one
	@$(HACK_DIR)/go/mod-update.sh $(ARGS)

.PHONY: go-mod-verify
go-mod-verify: ## Verify module checksums and tidiness
	@$(HACK_DIR)/go/mod-verify.sh

##@ Generate

.PHONY: generate-all
generate-all: ## Run every code generator, in dependency order
	@$(HACK_DIR)/generate/all.sh

.PHONY: generate-all-validate
generate-all-validate: ## Check every generated file is current
	@$(HACK_DIR)/generate/all.sh --validate

.PHONY: generate-engine
generate-engine: ## Regenerate the engine's opcode-keyed dispatch tables
	@$(HACK_DIR)/generate/engine.sh

.PHONY: generate-engine-validate
generate-engine-validate: ## Check the committed engine tables are current
	@$(HACK_DIR)/generate/engine.sh --validate

.PHONY: generate-asmgen
generate-asmgen: ## Regenerate the dispatch assembly and its offset headers
	@$(HACK_DIR)/generate/asmgen.sh

.PHONY: generate-asmgen-validate
generate-asmgen-validate: ## Check the committed .s/.h files are current
	@$(HACK_DIR)/generate/asmgen.sh --validate

.PHONY: generate-symtab
generate-symtab: ## Regenerate the named-scalar pool tags
	@$(HACK_DIR)/generate/symtab.sh

.PHONY: generate-symtab-validate
generate-symtab-validate: ## Check the committed named-scalar tags are current
	@$(HACK_DIR)/generate/symtab.sh --validate

.PHONY: generate-flatc
generate-flatc: ## Regenerate the FlatBuffers bindings
	@$(HACK_DIR)/generate/flatc.sh

.PHONY: generate-flatc-validate
generate-flatc-validate: ## Check the committed FlatBuffers bindings are current
	@$(HACK_DIR)/generate/flatc.sh --validate

.PHONY: generate-symbols
generate-symbols: ## Regenerate the vendored stdlib symbol tables
	@$(HACK_DIR)/generate/symbols.sh interp

.PHONY: generate-symbols-validate
generate-symbols-validate: ## Check the vendored stdlib symbol tables are current
	@$(HACK_DIR)/generate/symbols.sh interp --validate

.PHONY: generate-selfhost-symbols
generate-selfhost-symbols: ## Regenerate the self-hosting symbol tables
	@$(HACK_DIR)/generate/symbols.sh selfhost

.PHONY: generate-selfhost-symbols-validate
generate-selfhost-symbols-validate: ## Check the self-hosting symbol tables are current
	@$(HACK_DIR)/generate/symbols.sh selfhost --validate

##@ Pre-submit

.PHONY: check
check: lint-vet lint-vet-tags lint-go lint-scripts go-mod-verify generate-all-validate ## Every gate that does not run a test suite

.PHONY: check-all
check-all: check test test-golden ## check plus the test suites and goldens

##@ Clean

.PHONY: clean
clean: ## Clean build artefacts
	rm -rf $(BIN_DIR)
	rm -rf .bench
	rm -f $(TMPDIR)/pipit-coverage-*.out
